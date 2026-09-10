package plist

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// Handler 把 plist 解析包成 apiserver.ToolHandler。
//
// 为什么值得暴露给 agent:iOS 设备上一大半配置和状态都存在 plist 里,
// 而其中最有价值的那部分是二进制的 bplist —— 直接 cat 出来是一团乱码。
// 更麻烦的是 NSKeyedArchiver:就算解出了 plist,看到的也是一张对象平表
// 加一堆 UID 引用,得自己顺着下标拼回去。这两件事都不是 agent 该现场手推的。
type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (h *Handler) Name() string  { return "plist-parse" }
func (h *Handler) Title() string { return "plist 解析" }
func (h *Handler) Description() string {
	return "解析 Apple 属性列表(二进制 bplist 与 XML plist),自动识别格式;" +
		"顶层是 NSKeyedArchiver 归档时顺带拆包成正常的对象树。" +
		"可以给文件路径,也可以直接给从 SQLite 里抠出来的字节"
}
func (h *Handler) Methods() []string { return []string{http.MethodPost} }

type parseRequest struct {
	// Path plist 文件路径
	Path string `json:"path,omitempty"`
	// Data 直接给字节;和 Path 二选一
	Data string `json:"data,omitempty"`
	// Encoding Data 的编码:hex / base64 / blob
	Encoding string `json:"encoding,omitempty"`
	// SubPath 只看这条路径下的子树
	SubPath string `json:"subPath,omitempty"`
	// KeepNSKeyed 保留 NSKeyedArchiver 原始结构,不拆包
	KeepNSKeyed bool `json:"keepNSKeyed,omitempty"`
	// MaxArray / MaxData 覆盖默认上限
	MaxArray int `json:"maxArray,omitempty"`
	MaxData  int `json:"maxData,omitempty"`
}

// maxFileSize 文件大小上限。
// plist 正常都是几 KB 到几 MB;真碰到超过这个数的,
// 多半是路径给错了(指到一个数据库或者视频上),读进内存不划算
const maxFileSize = 64 << 20

func (h *Handler) Handle(_ context.Context, body []byte) ([]byte, error) {
	var req parseRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, errors.New("请求体不是合法 JSON: " + err.Error())
		}
	}
	data, err := readInput(req)
	if err != nil {
		return nil, err
	}

	res, err := Parse(data, Options{
		UnwrapNSKeyed: !req.KeepNSKeyed,
		MaxArray:      req.MaxArray,
		MaxData:       req.MaxData,
		SubPath:       req.SubPath,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

// readInput 从 path 或 data 里拿到原始字节
func readInput(req parseRequest) ([]byte, error) {
	switch {
	case req.Path != "" && req.Data != "":
		return nil, errors.New("path 和 data 只能给一个")
	case req.Path != "":
		st, err := os.Stat(req.Path)
		if err != nil {
			return nil, err
		}
		if st.IsDir() {
			return nil, fmt.Errorf("%s 是个目录,不是 plist 文件", req.Path)
		}
		if st.Size() > maxFileSize {
			return nil, fmt.Errorf("文件 %d 字节,超过 %d 的上限 —— 确认一下路径指对了没有",
				st.Size(), int64(maxFileSize))
		}
		return os.ReadFile(req.Path)
	case req.Data != "":
		return decodeInline(req.Data, req.Encoding)
	}
	return nil, errors.New("要么给 path,要么给 data")
}

var reBlobLiteral = regexp.MustCompile(`(?is)x'([0-9a-f\s]*)'`)

// decodeInline 把内联给的字节还原出来。
//
// 编码可以不给:三种形式长得完全不一样,认错的概率比让调用方多填一个字段的成本低。
// 给了就按给的来 —— 显式的意图优先于猜测。
func decodeInline(data, encoding string) ([]byte, error) {
	s := strings.TrimSpace(data)
	if s == "" {
		return nil, errors.New("data 为空")
	}
	switch strings.ToLower(encoding) {
	case "blob":
		return parseBlobLiteral(s)
	case "hex":
		return parseHex(s)
	case "base64":
		return parseBase64(s)
	case "":
		if reBlobLiteral.MatchString(s) {
			return parseBlobLiteral(s)
		}
		// bplist 的 base64 一定以 "YnBsaXN0" 开头("bplist" 的 base64),
		// 这个特征比"看着像 hex"强得多,先用它定
		if strings.HasPrefix(s, "YnBsaXN0") {
			return parseBase64(s)
		}
		if b, err := parseHex(s); err == nil {
			return b, nil
		}
		return parseBase64(s)
	}
	return nil, fmt.Errorf("不认识的 encoding %q,可选 hex / base64 / blob", encoding)
}

func parseBlobLiteral(s string) ([]byte, error) {
	m := reBlobLiteral.FindStringSubmatch(s)
	if m == nil {
		return nil, errors.New("不是 SQLite 的 X'...' 字面量")
	}
	return parseHex(m[1])
}

var reHexJunk = regexp.MustCompile(`(?i)^0x|[\s,]|\\x`)

func parseHex(s string) ([]byte, error) {
	clean := reHexJunk.ReplaceAllString(s, "")
	if clean == "" {
		return nil, errors.New("没有有效的十六进制字符")
	}
	if len(clean)%2 != 0 {
		return nil, fmt.Errorf("hex 字符数是奇数(%d)", len(clean))
	}
	return hex.DecodeString(clean)
}

func parseBase64(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}

func (h *Handler) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "plist 文件的绝对路径。和 data 二选一",
			},
			"data": map[string]any{
				"type": "string",
				"description": "直接给字节,用于从 SQLite BLOB 列里抠出来的 bplist。" +
					"和 path 二选一",
			},
			"encoding": map[string]any{
				"type": "string",
				"enum": []string{"hex", "base64", "blob"},
				"description": "data 的编码。不给会自动认:blob 是 SQLite 的 X'0A0B...' 字面量," +
					"可以原样贴进来",
			},
			"subPath": map[string]any{
				"type": "string",
				"description": "只返回这条路径下的子树,段之间用 / 分隔,如 " +
					"NS.objects/0/title。数组用下标。一个 plist 有几千个键时用它收窄",
			},
			"keepNSKeyed": map[string]any{
				"type": "boolean",
				"description": "默认会把 NSKeyedArchiver 归档拆成正常对象树。" +
					"想看原始的 $objects 平表和 UID 引用时才设成 true",
			},
			"maxArray": map[string]any{
				"type":        "integer",
				"description": "数组最多展开多少项,默认 500。被截断的地方会有 __truncated 标记",
			},
			"maxData": map[string]any{
				"type":        "integer",
				"description": "单个 data 块最多给多少字节,默认 1024。超出的只给开头一段",
			},
		},
	}
}
