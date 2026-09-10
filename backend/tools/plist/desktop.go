package plist

import (
	"errors"
	"fmt"
	"os"

	hplist "howett.net/plist"
)

// 桌面工具页的入口。
//
// 和 MCP 那头走的是同一个 decodeDocument / UnwrapNSKeyedArchive —— 这正是把解析
// 从前端搬过来的意义:之前浏览器里一份 TypeScript、Go 里一份,同一个文件两边
// 解出不一样的结果时谁也不知道该信哪个。现在只有一份。
//
// 和 handler.go 的区别在于给谁看:
//   - 三个视图一次给全(XML 原文 / 拆包后 / 原始结构),页面切标签不用重新解析
//   - 不截断:人在页面上是要翻完的,少给一项都是漏。MCP 那边截断是因为
//     多一屏数据就是少一屏别的东西,页面没有这个约束

// DesktopResult 桌面页需要的全部信息
type DesktopResult struct {
	// Format binary / xml / openstep / gnustep
	Format string `json:"format"`
	// NSKeyed 顶层是不是 NSKeyedArchiver 归档
	NSKeyed bool `json:"nsKeyed"`
	// XML XML 形式的原文。输入本来就是 XML 时是原样,
	// 二进制输入时是按解析结果反向生成的
	XML string `json:"xml"`
	// XMLError 反向生成 XML 失败时的原因;失败不影响另外两个视图
	XMLError string `json:"xmlError,omitempty"`
	// Raw 原始结构,没做 NSKeyedArchiver 拆包
	Raw any `json:"raw"`
	// Parsed 拆包后的结构;不是归档时和 Raw 一样
	Parsed any `json:"parsed"`
	// Notes 循环引用、越界 UID 之类需要提醒的事
	Notes []string `json:"notes"`
}

// unlimited 传给 render 表示不设上限。
// 页面上截断等于悄悄骗人:看不到的那部分和"不存在"长得一模一样
const unlimited = -1

// maxDesktopFileSize 桌面页打开的文件上限
const maxDesktopFileSize = 64 << 20

// ParseForDesktop 解析一段 plist 字节,三个视图一次给全
func ParseForDesktop(data []byte) (*DesktopResult, error) {
	if len(data) == 0 {
		return nil, errors.New("输入为空")
	}
	raw, format, err := decodeDocument(data)
	if err != nil {
		return nil, err
	}

	out := &DesktopResult{
		Format:  formatName(format),
		NSKeyed: IsNSKeyedArchive(raw),
		Notes:   []string{},
	}
	opt := Options{MaxArray: unlimited, MaxData: unlimited}

	rendered, notes := render(raw, opt)
	out.Raw = rendered
	out.Notes = append(out.Notes, notes...)

	if out.NSKeyed {
		unwrapped, unotes := UnwrapNSKeyedArchive(raw)
		parsed, pnotes := render(unwrapped, opt)
		out.Parsed = parsed
		out.Notes = append(out.Notes, unotes...)
		out.Notes = append(out.Notes, pnotes...)
	} else {
		out.Parsed = rendered
	}

	// XML 原文:输入就是 XML 的话原样给回去 —— 反向生成出来的和用户手里那份
	// 缩进、键顺序都不一样,拿去和原文比对会白白多出一堆假差异
	if out.Format == "xml" {
		out.XML = string(data)
	} else if xml, err := hplist.MarshalIndent(raw, hplist.XMLFormat, "\t"); err != nil {
		out.XMLError = fmt.Sprintf("反向生成 XML 失败: %v", err)
	} else {
		out.XML = string(xml)
	}
	return out, nil
}

// ParseFileForDesktop 读一个文件再解析。
// 走路径而不是把内容塞过桥:几十 MB 的文件转成 base64 再传一遍纯属浪费
func ParseFileForDesktop(path string) (*DesktopResult, error) {
	if path == "" {
		return nil, errors.New("没有选择文件")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s 是个目录,不是 plist 文件", path)
	}
	if st.Size() > maxDesktopFileSize {
		return nil, fmt.Errorf("文件 %.2f MB,超过 %d MB 的上限",
			float64(st.Size())/(1<<20), maxDesktopFileSize>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseForDesktop(data)
}

// ParseEncodedForDesktop 解析用户粘进来的 base64 / hex / SQLite X'..' 字面量。
// encoding 为空时自动认,规则见 decodeInline
func ParseEncodedForDesktop(data, encoding string) (*DesktopResult, error) {
	b, err := decodeInline(data, encoding)
	if err != nil {
		return nil, err
	}
	return ParseForDesktop(b)
}
