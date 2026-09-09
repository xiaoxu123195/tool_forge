package aichat

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 跨会话搜索的几个上限。
//
// 都是为了"结果要能一眼扫完"而不是"尽可能全":一次问话里同一个词出现二十遍,
// 二十行结果全指向同一条会话,反而把别的会话挤没了。
const (
	searchMaxConvs       = 50 // 最多返回多少条会话
	searchHitsPerConv    = 5  // 单条会话里最多列几处命中
	searchSnippetPadding = 30 // 摘录里命中词前后各留多少字
)

// SearchHit 一处命中。before / match / after 是切好的三段,
// 前端直接把 match 那段套个高亮就行 —— 让它自己去算下标容易和后端切法不一致
type SearchHit struct {
	MessageID string `json:"messageId"`
	Role      string `json:"role"`
	Before    string `json:"before"`
	Match     string `json:"match"`
	After     string `json:"after"`
	CreatedAt int64  `json:"createdAt"`
}

// SearchResult 一条会话里的全部命中
type SearchResult struct {
	ConvID    string `json:"convId"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updatedAt"`
	// TitleMatch 命中的是标题本身。标题没有"上下文"可摘,单独标一下,
	// 前端才知道该显示"标题匹配"而不是一段空摘录
	TitleMatch bool        `json:"titleMatch,omitempty"`
	Hits       []SearchHit `json:"hits"`
	// More 这条会话里还有多少处命中没列出来
	More int `json:"more,omitempty"`
}

// SearchConversations 在所有会话里找一段文字。
//
// 每次都把会话文件全读一遍。没建索引是有意的:桌面端的会话数量是几十条这个量级,
// 全读一遍也就几毫秒,而一个要维护、要考虑失效、要跟着落盘走的索引,
// 复杂度远超它能省下的时间。真到了几千条那天再说。
//
// 搜的范围是标题 + 正文 + 思考文本。工具调用的原始结果不搜 ——
// 那是几万字的网页正文,搜出来全是噪音,而人想找的从来不是它。
func (s *Service) SearchConversations(query string) ([]SearchResult, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	d, err := dataDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(d, "conversations"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	out := make([]SearchResult, 0, 16)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var c Conversation
		if err := readJSON(filepath.Join(d, "conversations", e.Name()), &c); err != nil || c.ID == "" {
			continue
		}
		r := searchOne(&c, q)
		if r != nil {
			out = append(out, *r)
		}
	}
	// 近期改过的排前面。命中数多的排前面听起来更"相关",但实际不是:
	// 一篇长文里出现十次,并不比昨天那条对话里出现一次更值得先看
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	if len(out) > searchMaxConvs {
		out = out[:searchMaxConvs]
	}
	return out, nil
}

func searchOne(c *Conversation, q string) *SearchResult {
	// Hits 必须初始化成空切片,不能留 nil。
	//
	// Go 的 nil 切片 json.Marshal 出来是 null,不是 [] —— 前端那边
	// r.hits.length 当场就是 "Cannot read properties of null"。
	// 只命中标题、正文一处都没有的会话正好走这条路(搜个预设名就能撞上),
	// 是整个界面白屏的那种崩。
	r := SearchResult{
		ConvID:     c.ID,
		Title:      c.Title,
		UpdatedAt:  c.UpdatedAt,
		TitleMatch: strings.Contains(strings.ToLower(c.Title), q),
		Hits:       []SearchHit{},
	}
	total := 0
	for _, m := range c.Messages {
		if m.Role == RoleClear || m.Role == RoleSystem || m.Role == RoleTool {
			continue
		}
		hay := m.Content
		if t := thinkingOf(m); t != "" {
			hay += "\n" + t
		}
		idx := indexFold(hay, q)
		if idx < 0 {
			continue
		}
		total++
		if len(r.Hits) >= searchHitsPerConv {
			continue // 还要接着数,好告诉用户"另有 N 处"
		}
		before, match, after := snippet(hay, idx, len(q))
		r.Hits = append(r.Hits, SearchHit{
			MessageID: m.ID,
			Role:      m.Role,
			Before:    before,
			Match:     match,
			After:     after,
			CreatedAt: m.CreatedAt,
		})
	}
	if total == 0 && !r.TitleMatch {
		return nil
	}
	r.More = total - len(r.Hits)
	return &r
}

// indexFold 不分大小写地找 q(q 必须already 小写),返回字节下标。
//
// 直接对 ToLower 的结果取下标有个坑:极个别字符转小写后字节数会变
// (土耳其语的 İ 就是),下标一错,后面按下标切原文就会切在半个字符上。
// 长度不一致时退回大小写敏感的查找 —— 宁可少命中一次,也不要切出乱码。
func indexFold(hay, q string) int {
	low := strings.ToLower(hay)
	if len(low) != len(hay) {
		return strings.Index(hay, q)
	}
	return strings.Index(low, q)
}

// snippet 按**字符**(不是字节)在命中词前后各截一段。
// 用字节切的话中文会被拦腰砍成乱码
func snippet(hay string, idx, qlen int) (before, match, after string) {
	head := []rune(hay[:idx])
	if len(head) > searchSnippetPadding {
		head = head[len(head)-searchSnippetPadding:]
	}
	tailStart := idx + qlen
	if tailStart > len(hay) {
		tailStart = len(hay)
	}
	tail := []rune(hay[tailStart:])
	if len(tail) > searchSnippetPadding {
		tail = tail[:searchSnippetPadding]
	}
	return collapseSpace(string(head)), hay[idx:tailStart], collapseSpace(string(tail))
}

// collapseSpace 摘录里的换行会把一行撑成好几行,统一压成空格
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
