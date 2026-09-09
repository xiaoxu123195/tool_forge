package aichat

import (
	"context"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// titleTimeout 起标题的超时。比语言识别宽松一点:标题模型可能是个会思考的,
// 而这件事跑在后台,慢几秒没人等它
const titleTimeout = 60 * time.Second

// titlePrompt 起标题的指令。
//
// 几条要求都是被真实输出教出来的:
//   - 不写"标题:"前缀 —— 模型很爱加
//   - 不加引号 —— 中英文引号都见过
//   - 用对话本身的语言 —— 中文提问被起一个英文标题很出戏
//   - 只要一行 —— 有些模型会先解释再给标题
const titlePrompt = `为下面这段对话起一个标题。

要求:
- 用对话本身使用的语言
- 概括这段对话在谈什么,不超过 16 个字
- 不要用"关于""讨论""对话"这类没信息量的词开头
- 只输出标题本身:不要引号、不要"标题:"前缀、不要句号、不要任何解释

对话:
`

// maybeAutoTitle 首轮问答结束后,让模型给这条会话起个像样的标题。
//
// 触发条件卡得很紧,三条缺一不可:
//  1. 设置里没关掉
//  2. 这条会话的标题还是自动来的(用户手工命名过就免谈)
//  3. 整条会话只有一轮问答 —— 起标题看的是"这段对话在谈什么",
//     聊到第十轮再改标题,用户眼里就是列表自己在乱动
//
// 起成功后把 TitleAuto 置 false:一条会话只自动起一次名。之后重新生成首条回答
// 也不会再触发,省掉一次白花的请求。
func (s *Service) maybeAutoTitle(convID string) {
	cfg := s.snapshotConfig()
	if cfg.AutoTitleOff {
		return
	}
	c, err := loadConversation(convID)
	if err != nil || !c.TitleAuto {
		return
	}
	user, assistant, ok := firstExchange(c)
	if !ok {
		return
	}

	// 专用标题模型:两个字段都填、供应商还启用着才用,否则退回会话自己的模型。
	// 退回而不是放弃 —— 用户把标题模型删了之后功能悄悄失效是最难查的那种问题。
	provID, modelID := cfg.TitleProviderID, cfg.TitleModelID
	if provID == "" || modelID == "" {
		provID, modelID = c.ProviderID, c.ModelID
	}
	prov, err := s.providerSnapshot(provID)
	if err != nil || !prov.Enabled {
		if provID == c.ProviderID {
			return
		}
		prov, err = s.providerSnapshot(c.ProviderID)
		if err != nil || !prov.Enabled {
			return
		}
		modelID = c.ModelID
	}

	raw, err := oneShot(context.Background(), oneShotRequest{
		Provider:         prov,
		ModelID:          modelID,
		Prompt:           titlePrompt + "用户: " + clip(user, 600) + "\n助手: " + clip(assistant, 600),
		Timeout:          titleTimeout,
		MinimalReasoning: true,
	})
	if err != nil {
		return // 起不出来就留着截出来的那个,不打扰用户
	}
	title := sanitizeTitle(raw)
	if title == "" {
		return
	}

	// 重新读盘再写:这几秒里用户可能已经重命名、甚至删了这条会话。
	// 拿之前那份内存副本直接存盘会把他的改动盖掉
	fresh, err := loadConversation(convID)
	if err != nil || !fresh.TitleAuto {
		return
	}
	fresh.Title = title
	fresh.TitleAuto = false
	// 不动 UpdatedAt:标题是这轮问答的产物,那一刻的活动时间已经记过了。
	// 再刷一次只会让会话在列表里毫无理由地又跳一次
	if err := saveConversation(fresh); err != nil {
		return
	}
	if s.ctx != nil {
		wailsruntime.EventsEmit(s.ctx, EventTitlePrefix+convID, title)
	}
}

// snapshotConfig 取一份配置副本(带懒加载)
func (s *Service) snapshotConfig() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return Config{}
	}
	return s.config
}

// firstExchange 取"第一轮问答"的正文,并顺带判断当前是不是刚好只有这一轮。
//
// 判断依据是 user 消息的条数而不是消息总数:一轮里可能夹着工具调用产生的
// tool 消息和多条 assistant 消息,按总数算永远对不上。
func firstExchange(c *Conversation) (user, assistant string, ok bool) {
	users := 0
	for _, m := range c.Messages {
		switch m.Role {
		case RoleUser:
			users++
			if users == 1 {
				user = m.Content
			}
		case RoleAssistant:
			if m.Content != "" {
				assistant = m.Content
			}
		}
	}
	if users != 1 || strings.TrimSpace(user) == "" || strings.TrimSpace(assistant) == "" {
		return "", "", false
	}
	return user, assistant, true
}

// clip 按字符(不是字节)截断,中文才不会被切成半个字
func clip(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

// titleTrimCutset 标题两端要削掉的装饰字符:各种引号、书名号、方括号,
// 以及模型爱加的结尾标点
const titleTrimCutset = " \t\"'`*#《》「」『』“”‘’()【】[]()。.!!??,,:;、"

// sanitizeTitle 把模型给的回答收拾成一个能直接进侧边栏的标题。
//
// 模型不听话的方式很有限,但每一种都见过,所以逐条治:
//   - 先解释再给标题 → 只取最后一个非空行(解释在前、结论在后是常见排版)
//   - 加"标题:"前缀 → 去掉
//   - 套引号 / 书名号 / 加粗星号 → 从两端削
//   - 干脆答非所问写了一整段 → 长度兜底,超了就截
//
// 返回空串表示"这结果不能用",调用方会保留原来那个截出来的标题。
func sanitizeTitle(raw string) string {
	// 只取最后一个非空行。取第一行会踩到"好的,这是标题:"那种开场白
	line := ""
	for _, l := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			line = t
		}
	}
	// 去前缀。冒号后面才是正文,但只在冒号靠前时切 —— 否则
	// "Go 和 Rust:选哪个"这种本身带冒号的好标题会被砍掉前半截
	for _, prefix := range []string{"标题:", "标题:", "Title:", "title:"} {
		if i := strings.Index(line, prefix); i >= 0 && i < 4 {
			line = line[i+len(prefix):]
		}
	}
	line = strings.TrimSpace(line)
	line = strings.Trim(line, titleTrimCutset)
	line = strings.Join(strings.Fields(line), " ") // 折叠内部的连续空白
	if line == "" {
		return ""
	}
	r := []rune(line)
	if len(r) > 24 {
		return strings.TrimRight(string(r[:24]), " ") + "…"
	}
	return line
}
