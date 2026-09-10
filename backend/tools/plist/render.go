package plist

import (
	"encoding/base64"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	hplist "howett.net/plist"
)

// 解析出来的值里有几样东西 json.Marshal 处理得不对或者不好读:
//   - []byte 会变成一长串 base64,一个几 MB 的附件能把整个回复撑满
//   - time.Time 默认是 RFC3339,这个没问题,但要统一到 UTC
//   - hplist.UID 是个 uint64,序列化出来就是个光秃秃的数字,
//     看不出它其实是"指向第 N 个对象的引用"
//
// 所以过一道 render,顺便把大数组和大 data 截断。
// 截断的地方都留标记 —— 悄悄少给数据比给少了更糟。

const (
	// defaultMaxArray 数组最多展开多少项。
	// 归档里一个 NS.objects 有上万项是常事,而调用方通常只想看前面几条
	defaultMaxArray = 500
	// defaultMaxData 单个 data 块最多给多少字节。
	// 1KB 足够看清 token / UUID / 小结构,再大的多半是图片或整段二进制
	defaultMaxData = 1024
	// renderMaxDepth 渲染递归深度上限,防御构造出来的深层嵌套
	renderMaxDepth = 512
)

type renderer struct {
	maxArray int
	maxData  int
	notes    []string
}

func render(v any, opt Options) (any, []string) {
	r := &renderer{
		maxArray: limitOr(opt.MaxArray, defaultMaxArray),
		maxData:  limitOr(opt.MaxData, defaultMaxData),
	}
	return r.walk(v, 0), r.notes
}

// limitOr 解释上限:0 = 用默认值,负数 = 不设限,正数 = 就用它。
// 桌面页传负数 —— 页面上悄悄截断等于骗人,看不到的那部分和"不存在"长得一模一样
func limitOr(v, def int) int {
	switch {
	case v == 0:
		return def
	case v < 0:
		return math.MaxInt
	}
	return v
}

func (r *renderer) walk(v any, depth int) any {
	if depth > renderMaxDepth {
		r.note(fmt.Sprintf("嵌套超过 %d 层,已停止展开", renderMaxDepth))
		return "__max_depth__"
	}
	switch n := v.(type) {
	case nil:
		return nil
	case []byte:
		return r.data(n)
	case time.Time:
		return n.UTC().Format(time.RFC3339Nano)
	case hplist.UID:
		// 只有关掉拆包时才会走到这里 —— 明确标出它是引用而不是数字
		return map[string]any{"__uid": uint64(n)}
	case float32:
		return float64(n)
	case []any:
		return r.array(n, depth)
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, item := range n {
			out[k] = r.walk(item, depth+1)
		}
		return out
	}
	return v
}

func (r *renderer) array(items []any, depth int) []any {
	limit := len(items)
	truncated := false
	if limit > r.maxArray {
		limit = r.maxArray
		truncated = true
	}
	out := make([]any, 0, limit+1)
	for _, item := range items[:limit] {
		out = append(out, r.walk(item, depth+1))
	}
	if truncated {
		out = append(out, map[string]any{
			"__truncated": fmt.Sprintf("还有 %d 项未展开", len(items)-limit),
		})
		r.note(fmt.Sprintf("有数组超过 %d 项被截断,需要全部内容时调大 maxArray", r.maxArray))
	}
	return out
}

func (r *renderer) data(b []byte) map[string]any {
	out := map[string]any{"__size": len(b)}
	if len(b) <= r.maxData {
		out["__data"] = base64.StdEncoding.EncodeToString(b)
		return out
	}
	out["__data"] = base64.StdEncoding.EncodeToString(b[:r.maxData])
	out["__truncated"] = fmt.Sprintf("只给了前 %d 字节,共 %d 字节", r.maxData, len(b))
	r.note(fmt.Sprintf("有 data 块超过 %d 字节被截断,需要完整内容时调大 maxData", r.maxData))
	return out
}

func (r *renderer) note(s string) {
	for _, n := range r.notes {
		if n == s {
			return
		}
	}
	r.notes = append(r.notes, s)
}

// selectPath 按 a/b/0/c 这样的路径钻进结果里。
//
// 存在的理由和 MMKV 的 keyFilter 一样:一个 plist 可能有几千个键,
// 调用方多半只关心其中一支。让它把路径写出来,比让它把整棵树读一遍再挑要省得多。
//
// 一段路径是键还是下标,由它所在那一层的容器决定,不看它长什么样:
// 字典层里 "0" 就是键名 "0",数组层里才当下标。
// 反过来"看着像数字就当下标"会把名字真叫 "0" 的键跳过去。
//
// 麻烦的是分隔符本身:真实 plist 里带斜杠的键太常见了 ——
// Apple 自己的 types.plist 整张表的键就是 "application/pdf" 这种 MIME type,
// 按斜杠一刀切下去,这类键一个都取不到。所以字典层允许把连续几段拼回一个键名,
// 拼得越长越优先,走不通再回退试短的。见 walkSegments。
func selectPath(v any, path string) (any, error) {
	segs := make([]string, 0, 8)
	for _, seg := range strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	if len(segs) == 0 {
		return v, nil
	}
	budget := pathSearchBudget
	return walkSegments(v, segs, nil, &budget)
}

// pathSearchBudget 回退搜索最多试多少条分支。
// 正常路径几步就到底了;设这个闸是因为解析的是别人设备上的文件,
// 一个键互相是前缀的畸形字典能让回退搜索指数膨胀
const pathSearchBudget = 10000

func walkSegments(cur any, segs, walked []string, budget *int) (any, error) {
	if len(segs) == 0 {
		return cur, nil
	}
	if *budget <= 0 {
		return nil, fmt.Errorf("路径 %s 处分支太多,已放弃搜索 —— 把路径写得更完整一些",
			pathSoFar(walked))
	}
	*budget--

	switch n := cur.(type) {
	case map[string]any:
		// 长的优先:先把剩下所有段当成一个键名试,不行再逐步缩短。
		// 后面走不通会退回来试更短的 —— 不回退的话,
		// 一个同时存在 "a" 和 "a/b" 两个键的文件会把调用方引到死路上
		var deepestErr error
		for take := len(segs); take >= 1; take-- {
			key := strings.Join(segs[:take], "/")
			next, ok := n[key]
			if !ok {
				continue
			}
			res, err := walkSegments(next, segs[take:], appendPath(walked, key), budget)
			if err == nil {
				return res, nil
			}
			if deepestErr == nil {
				deepestErr = err
			}
		}
		if deepestErr != nil {
			return nil, deepestErr
		}
		return nil, fmt.Errorf("路径 %s 处没有键 %q,这一层可选的键有:%s",
			pathSoFar(walked), segs[0], strings.Join(sortedKeys(n, 20), ", "))

	case []any:
		idx, err := strconv.Atoi(segs[0])
		if err != nil {
			return nil, fmt.Errorf("路径 %s 处是数组,%q 不是合法下标", pathSoFar(walked), segs[0])
		}
		if idx < 0 || idx >= len(n) {
			return nil, fmt.Errorf("路径 %s 处下标 %d 越界(共 %d 项)", pathSoFar(walked), idx, len(n))
		}
		return walkSegments(n[idx], segs[1:], appendPath(walked, segs[0]), budget)
	}
	return nil, fmt.Errorf("路径 %s 处已经是标量,不能再往下取 %q", pathSoFar(walked), segs[0])
}

// appendPath 复制一份再追加。
// 直接 append 到 walked 上,回退时几个分支会共用同一块底层数组,
// 报错里的路径就会串味
func appendPath(walked []string, seg string) []string {
	out := make([]string, len(walked), len(walked)+1)
	copy(out, walked)
	return append(out, seg)
}

func pathSoFar(walked []string) string {
	if len(walked) == 0 {
		return "根"
	}
	return "/" + strings.Join(walked, "/")
}

// sortedKeys 报错时把这一层有哪些键列出来。
// 只列前 max 个:一个字典有几千个键时,把它们全打出来对定位没有帮助
func sortedKeys(m map[string]any, max int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > max {
		return append(keys[:max:max], fmt.Sprintf("…(共 %d 个)", len(m)))
	}
	return keys
}
