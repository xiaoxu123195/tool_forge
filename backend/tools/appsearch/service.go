package appsearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Tool-Forge"

// Service 汇聚多源搜索
type Service struct {
	client *http.Client
	// icons 下图标原图专用(见 icon.go):跳转到本机或内网的地址不跟
	icons *http.Client
}

// New 构造 Service；client 内部注意把 Transport 的 Proxy 设成 nil，
// 避免 Windows 下读取 IE 代理而在 TUN 模式下打到没开的 HTTP 代理端口。
func New() *Service {
	transport := &http.Transport{
		Proxy:                 nil, // 显式不走系统代理
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &Service{
		client: &http.Client{Timeout: 12 * time.Second, Transport: transport},
		// 和搜索走同一条路:Google Play 能搜出来,它的图标就下得下来
		icons: newIconClient(transport),
	}
}

// Search 并发执行所有请求的源。任一源失败不影响其它源返回。
func (s *Service) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		return nil, errors.New("keyword 不能为空")
	}
	sources := req.Sources
	if len(sources) == 0 {
		sources = defaultSources()
	}
	country := req.Country
	if country == "" {
		country = "cn"
	}
	limit := req.LimitPerSource
	if limit <= 0 {
		limit = DefaultLimitPerSource
	}
	if limit > MaxLimitPerSource {
		limit = MaxLimitPerSource
	}

	var (
		mu       sync.Mutex
		items    []SearchResultItem
		statuses []SourceStatus
		wg       sync.WaitGroup
	)

	for _, src := range sources {
		src := src
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := SourceStatus{Source: src}
			// 关键:每个源都兜住 panic。任一源解析出错只记为该源失败,
			// 绝不能让裸 goroutine 里的 panic 掀翻整个 app。
			results := runSourceSafe(func() ([]SearchResultItem, error) {
				return s.runSource(ctx, src, keyword, country, req)
			}, &status, limit)
			mu.Lock()
			statuses = append(statuses, status)
			items = append(items, results...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	// 保持 statuses 顺序稳定（按 sources 顺序）
	statuses = sortStatuses(sources, statuses)

	return &SearchResponse{
		Items:    items,
		Statuses: statuses,
	}, nil
}

// runSourceSafe 执行单个源并兜住 panic:成功填 status.OK/Count 并返回结果(截断到 limit),
// 出错或 panic 填 status.Error 返回 nil。保证任何源的异常都不会冒泡到 goroutine 顶层把 app 搞崩。
func runSourceSafe(run func() ([]SearchResultItem, error), status *SourceStatus, limit int) (out []SearchResultItem) {
	defer func() {
		if r := recover(); r != nil {
			status.OK = false
			status.Count = 0
			status.Error = fmt.Sprintf("源处理异常: %v", r)
			out = nil
		}
	}()
	results, err := run()
	if err != nil {
		// 说明类"错误"不算失败:源是通的,只是结果空得可疑
		var note *sourceNote
		if errors.As(err, &note) {
			status.OK = true
			status.Count = 0
			status.Note = note.msg
			return nil
		}
		status.Error = err.Error()
		return nil
	}
	// 每源截断到 limit;Count 反映截断后的真实数量
	if len(results) > limit {
		results = results[:limit]
	}
	status.OK = true
	status.Count = len(results)
	return results
}

func (s *Service) runSource(ctx context.Context, src SourceID, keyword, country string, req SearchRequest) ([]SearchResultItem, error) {
	switch src {
	case SourceITunes:
		return searchITunes(ctx, s.client, keyword, country, 20)
	case SourceQimaiIOS:
		return searchQimaiIOS(ctx, s.client, keyword, country)
	case SourceQimaiAndroid:
		return searchQimaiAndroid(ctx, s.client, keyword, country, req.Market, req.qimaiCredential)
	case SourceYingYongBao:
		return searchYingYongBao(ctx, s.client, keyword, 20)
	case SourceGooglePlay:
		// Google Play 固定 gl=us lang=en：gl=cn 会被 Google 或 GFW 拦截，
		// 国内用户搜 GP 的目的本来也是找海外 Android 包名。
		return searchGooglePlay(ctx, s.client, keyword, "us", "en")
	default:
		return nil, errSourceNotSupported(src)
	}
}

func defaultSources() []SourceID {
	return []SourceID{SourceITunes, SourceQimaiIOS, SourceQimaiAndroid, SourceYingYongBao, SourceGooglePlay}
}

func sortStatuses(order []SourceID, in []SourceStatus) []SourceStatus {
	idx := make(map[SourceID]int, len(order))
	for i, s := range order {
		idx[s] = i
	}
	out := make([]SourceStatus, len(in))
	copy(out, in)
	// insertion sort — 数量小
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && idx[out[j].Source] < idx[out[j-1].Source]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func errSourceNotSupported(src SourceID) error {
	return &unsupportedSourceError{src: src}
}

type unsupportedSourceError struct{ src SourceID }

func (e *unsupportedSourceError) Error() string {
	return "source not supported yet: " + string(e.src)
}
