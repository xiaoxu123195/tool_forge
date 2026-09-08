package aichat

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// 一个供应商可以配多把 API 密钥。这不是"备份密钥"那种冗余设计,而是两个很实际的需求:
//
//   - 免费额度按 key 计,几把轮着用能把额度摊开
//   - 某把 key 被限流 / 欠费 / 被吊销时,这次请求还能换一把接着发,而不是直接失败
//
// 选哪一把由轮转游标决定(见 keyAttempts),失败后按顺序换下一把。

// APIKeyEntry 密钥池里的一条。
//
// 之所以带 ID 而不是拿密钥本身当标识:用户会改密钥值(轮换、续期),
// 而检测结果、启用状态这些要跟着"这一条"走,不能跟着值走。
type APIKeyEntry struct {
	ID string `json:"id"`
	// Key 密钥明文。Wails 是本地应用,和 Provider.APIKey 一样明文存
	Key string `json:"key"`
	// Label 用户给的备注,如 "个人号" / "公司额度";纯展示用
	Label string `json:"label,omitempty"`
	// Disabled 默认零值即启用 —— 这样老数据反序列化出来就是可用的,不用额外迁移
	Disabled bool `json:"disabled,omitempty"`
}

// KeyCheckResult 一把密钥的检测结果
type KeyCheckResult struct {
	KeyID      string `json:"keyId"`
	OK         bool   `json:"ok"`
	StatusCode int    `json:"statusCode,omitempty"`
	DurationMs int    `json:"durationMs"`
	Message    string `json:"message,omitempty"`
}

// keyRotator 每个供应商一个轮转游标。
//
// 存在 Service 之外是因为它是纯运行时状态:重启后从头开始轮完全没问题,
// 不值得为它多写一个持久化文件,更不该混进 providers.json 让每次请求都触发写盘。
type keyRotator struct {
	mu sync.Mutex
	m  map[string]*uint64
}

var rotator = &keyRotator{}

// next 返回该供应商的下一个轮转序号(单调递增)
func (r *keyRotator) next(providerID string) uint64 {
	r.mu.Lock()
	c, ok := r.m[providerID]
	if !ok {
		if r.m == nil {
			r.m = map[string]*uint64{}
		}
		c = new(uint64)
		r.m[providerID] = c
	}
	r.mu.Unlock()
	return atomic.AddUint64(c, 1) - 1
}

// splitKeyString 把一段文本切成多把密钥。
//
// 用户粘贴过来的形态五花八门:每行一把、逗号分隔、从表格里复制带着制表符。
// 分号 / 空白也一并当分隔符 —— 真实密钥里不会出现这些字符。
func splitKeyString(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ',', ';', '\n', '\r', '\t', ' ', '\v', '\f':
			return true
		}
		return false
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// keyEntryID 由密钥值算一个稳定 ID。
//
// 用哈希而不是 uuid,是为了让"同一把密钥"在迁移、重复导入时落到同一条上 ——
// 用户把老的单密钥字段迁过来、又粘贴了一份包含它的列表,不该出现两条一模一样的。
// 只取前 12 位:够区分,又不至于让 providers.json 里全是长串。
func keyEntryID(key string) string {
	sum := sha1.Sum([]byte(key))
	return "k_" + hex.EncodeToString(sum[:])[:12]
}

// normalizeKeys 把 Provider 的密钥配置整理成规范的池:补 ID、去空、按 Key 去重。
//
// 两个字段的优先级是"有 APIKeys 就只认 APIKeys":
//
//	APIKeys 非空 → 它就是完整的池,APIKey 完全忽略
//	APIKeys 为空 → 从老的单密钥字段拆(它可能是用户用逗号塞进去的一串)
//
// 这条规则必须是非此即彼的。如果改成"两边合并",用户从列表里删掉一把密钥后,
// 它还会从 APIKey 字段里被重新合并回来 —— 删不掉的删除按钮。
//
// 返回的是新切片,不改动入参。
func normalizeKeys(p Provider) []APIKeyEntry {
	src := p.APIKeys
	if len(src) == 0 {
		for _, k := range splitKeyString(p.APIKey) {
			src = append(src, APIKeyEntry{Key: k})
		}
	}
	out := make([]APIKeyEntry, 0, len(src))
	seen := map[string]bool{}
	for _, e := range src {
		e.Key = strings.TrimSpace(e.Key)
		if e.Key == "" || seen[e.Key] {
			continue
		}
		seen[e.Key] = true
		if e.ID == "" {
			e.ID = keyEntryID(e.Key)
		}
		e.Label = strings.TrimSpace(e.Label)
		out = append(out, e)
	}
	return out
}

// enabledKeys 池子里当前可用的那些
func enabledKeys(keys []APIKeyEntry) []APIKeyEntry {
	out := make([]APIKeyEntry, 0, len(keys))
	for _, e := range keys {
		if !e.Disabled && strings.TrimSpace(e.Key) != "" {
			out = append(out, e)
		}
	}
	return out
}

// keyAttempts 返回这次请求可以依次尝试的密钥。
//
// 第一把由轮转游标选出(所以连续几次请求会均摊到不同密钥上),其余的按环形顺序跟在后面
// 作为失败后的备选。池子空时返回一个空 ID 的条目,让调用方照常走"密钥为空"的报错路径,
// 而不是在这里提前分叉。
func keyAttempts(p Provider) []APIKeyEntry {
	keys := enabledKeys(normalizeKeys(p))
	if len(keys) == 0 {
		return []APIKeyEntry{{Key: strings.TrimSpace(p.APIKey)}}
	}
	if len(keys) == 1 {
		return keys
	}
	start := int(rotator.next(p.ID) % uint64(len(keys)))
	out := make([]APIKeyEntry, 0, len(keys))
	out = append(out, keys[start:]...)
	out = append(out, keys[:start]...)
	return out
}

// pickKey 按轮转挑一把密钥,返回可以直接交给协议层的 Provider。
// 只发一次、失败不重试的场景(列模型、检测、翻译)用这个就够了。
func pickKey(p Provider) Provider {
	return withKey(p, keyAttempts(p)[0].Key)
}

// withKey 复制一份 Provider,把 APIKey 换成指定的那把。
//
// 这样协议层(openai.go / anthropic.go / gemini.go)完全不用知道密钥池的存在 ——
// 它们看到的永远是"这个 Provider 有一把密钥",轮转和failover 都在上层解决。
func withKey(p Provider, key string) Provider {
	p.APIKey = key
	return p
}

// isKeyLevelError 判断错误是不是"换把密钥可能就好了"的那种。
//
// 判断得保守一点:模型不存在、参数非法这些换 key 也没用,重试只是白白多打几次请求;
// 真正值得重试的是认证失败、额度耗尽、被限流这三类。
func isKeyLevelError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// HTTP 状态码是各协议统一格式化成 "HTTP %d: ..." 抛上来的
	for _, code := range []string{"http 401", "http 402", "http 403", "http 429"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	for _, kw := range []string{
		"invalid api key", "incorrect api key", "invalid_api_key",
		"authentication", "unauthorized", "permission denied",
		"quota", "insufficient", "rate limit", "rate_limit",
		"exceeded your current", "billing",
		"密钥", "余额", "配额", "限流", "认证失败",
	} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// ================ Service 侧 ================

// ListProviderKeys 返回某供应商的密钥池(已规范化)
func (s *Service) ListProviderKeys(providerID string) ([]APIKeyEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	p, idx := s.getProviderLocked(providerID)
	if idx < 0 {
		return nil, fmt.Errorf("供应商不存在: %s", providerID)
	}
	return normalizeKeys(*p), nil
}

// SaveProviderKeys 整体替换某供应商的密钥池。
//
// 整体替换而不是逐条增删:密钥列表很短,用户的操作(粘一批、删几条、调顺序)
// 落到接口上都是"新的完整列表",一次写盘比一串增量调用更难出现半成品状态。
func (s *Service) SaveProviderKeys(providerID string, keys []APIKeyEntry) ([]APIKeyEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	p, idx := s.getProviderLocked(providerID)
	if idx < 0 {
		return nil, fmt.Errorf("供应商不存在: %s", providerID)
	}
	p.APIKeys = normalizeKeys(Provider{APIKeys: keys})
	// 老字段跟着走第一把可用的:别处(旧版本、导出的配置)还会读它
	p.APIKey = ""
	if act := enabledKeys(p.APIKeys); len(act) > 0 {
		p.APIKey = act[0].Key
	}
	if err := saveProviders(s.providers); err != nil {
		return nil, err
	}
	return p.APIKeys, nil
}

// CheckProviderKeys 逐把检测密钥能否正常调用指定模型。
//
// keyIDs 为空时检测全部启用中的密钥。并发跑但限制在 4 路以内 ——
// 检测本身就是发真实请求,一次把十几把密钥全打出去容易撞上供应商的并发限制,
// 结果全是 429,反而把好密钥误判成坏的。
func (s *Service) CheckProviderKeys(providerID, modelID string, keyIDs []string) []KeyCheckResult {
	s.mu.Lock()
	if err := s.ensureLoaded(); err != nil {
		s.mu.Unlock()
		return nil
	}
	p, idx := s.getProviderLocked(providerID)
	if idx < 0 {
		s.mu.Unlock()
		return nil
	}
	provider := *p
	s.mu.Unlock()

	all := normalizeKeys(provider)
	want := map[string]bool{}
	for _, id := range keyIDs {
		want[id] = true
	}
	targets := make([]APIKeyEntry, 0, len(all))
	for _, e := range all {
		if len(want) > 0 {
			if want[e.ID] {
				targets = append(targets, e)
			}
			continue
		}
		if !e.Disabled {
			targets = append(targets, e)
		}
	}
	if len(targets) == 0 {
		return []KeyCheckResult{}
	}

	results := make([]KeyCheckResult, len(targets))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, e := range targets {
		wg.Add(1)
		go func(i int, e APIKeyEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := testProviderModelWithKey(withKey(provider, e.Key), modelID)
			results[i] = KeyCheckResult{
				KeyID:      e.ID,
				OK:         r.OK,
				StatusCode: r.StatusCode,
				DurationMs: r.DurationMs,
				Message:    r.Message,
			}
		}(i, e)
	}
	wg.Wait()
	return results
}

// testProviderModelWithKey 按端点类型分发到各协议的探测实现
func testProviderModelWithKey(p Provider, modelID string) TestResult {
	switch endpointFor(p.Type) {
	case EndpointGemini:
		return testGeminiModel(p, modelID)
	case EndpointAnthropic:
		return testAnthropicModel(p, modelID)
	case EndpointOpenAIChat:
		return testModel(p, modelID, false)
	default:
		return testModel(p, modelID, true)
	}
}
