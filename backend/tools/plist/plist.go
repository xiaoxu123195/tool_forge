// Package plist 解析 Apple 的属性列表:XML plist、二进制 bplist,
// 以及包在里面的 NSKeyedArchiver 归档。
//
// 为什么后端要有一份:桌面工具页那份是 TypeScript,只有人点得动。
// agent 从设备里提出来的 Info.plist / com.apple.*.plist / SQLite BLOB 里的
// bplist,得有个能从 MCP 调的入口。
//
// 二进制格式本身没有手写解析 —— 用的是 howett.net/plist,
// 也就是 wails 自己已经依赖的那个库(所以没有给构建引入新的版本)。
// bplist 的坑不少:UTF-16 字符串、int128、偏移表宽度随文件变,
// 手写一份的下场是"大部分文件能读,少数文件静悄悄读错"。
// NSKeyedArchiver 那层它不管,所以那部分是自己实现的,见 nskeyed.go。
package plist

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	hplist "howett.net/plist"
)

// Result 一次解析的全部产出
type Result struct {
	// Format 探测到的格式:binary / xml / openstep / gnustep
	Format string `json:"format"`
	// NSKeyed 顶层是不是一个 NSKeyedArchiver 归档
	NSKeyed bool `json:"nsKeyed"`
	// Unwrapped 是否真的做了拆包(NSKeyed 为真且调用方没关掉)
	Unwrapped bool `json:"unwrapped"`
	// SubPath 如果只返回了子树,这里是那条路径
	SubPath string `json:"subPath,omitempty"`
	// Value 解析结果,已经转成可以直接 json.Marshal 的形状
	Value any `json:"value"`
	// Notes 解析过程中值得说一声的事(截断、循环引用等)
	Notes []string `json:"notes"`
}

// formatName 把库里的格式常量翻成名字。
// 不直接用库的 FormatNames:那里面是英文的 "Binary"/"XML",
// 这里的取值要和前端工具页说的是同一套词。
func formatName(f int) string {
	switch f {
	case hplist.BinaryFormat:
		return "binary"
	case hplist.XMLFormat:
		return "xml"
	case hplist.OpenStepFormat:
		return "openstep"
	case hplist.GNUStepFormat:
		return "gnustep"
	}
	return "unknown"
}

// Options 解析选项
type Options struct {
	// UnwrapNSKeyed 顶层是 NSKeyedArchiver 时是否拆成正常的对象树。
	// 不拆的话拿到的是 $objects 平表加一堆 UID,基本没法读
	UnwrapNSKeyed bool
	// MaxArray 数组最多展开多少项,0 表示用默认值
	MaxArray int
	// MaxData 单个 data 块最多给多少字节的 base64,0 表示用默认值
	MaxData int
	// SubPath 只返回这条路径下的子树,如 root/NS.objects/0。
	// 空表示整棵树
	SubPath string
}

// DefaultOptions 默认按"给 agent 看"来配
func DefaultOptions() Options {
	return Options{UnwrapNSKeyed: true}
}

// Parse 解析一段 plist 字节。
func Parse(data []byte, opt Options) (*Result, error) {
	if len(data) == 0 {
		return nil, errors.New("输入为空")
	}
	raw, format, err := decodeDocument(data)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Format:  formatName(format),
		NSKeyed: IsNSKeyedArchive(raw),
		Notes:   []string{},
	}

	value := raw
	if res.NSKeyed && opt.UnwrapNSKeyed {
		unwrapped, notes := UnwrapNSKeyedArchive(raw)
		value = unwrapped
		res.Unwrapped = true
		res.Notes = append(res.Notes, notes...)
	}

	// 钻子树放在 render 之前:放在之后的话,先把整棵树按上限截一遍,
	// 想看的那一支可能正好被截没了
	if opt.SubPath != "" {
		sub, err := selectPath(value, opt.SubPath)
		if err != nil {
			return nil, err
		}
		value = sub
		res.SubPath = opt.SubPath
	}

	rendered, notes := render(value, opt)
	res.Value = rendered
	res.Notes = append(res.Notes, notes...)
	return res, nil
}

// decodeDocument 调库解析,并把 panic 兜住。
//
// howett 的 Decode 只把自己抛的 error 转成返回值,遇到 runtime.Error
// (越界之类)会原样再抛出去。
//
// 说清楚这层 recover 的性质:我用 go fuzz 打了 180 万条畸形输入,
// 一次都没触发过 —— 上游自己带了 fuzz 目标,这块是扎实的。
// 所以它不是在修一个已知的崩溃,是一道兜底:这条路径读的是从别人设备里
// 提出来的文件,而这个进程就是桌面 app 本身,真漏一个越界过去就是整个 app 没了。
// 代价是一个 defer,值。
func decodeDocument(data []byte) (value any, format int, err error) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(runtime.Error); ok {
				err = fmt.Errorf("plist 文件已损坏或不是合法的 plist(解析时越界: %v)", re)
				return
			}
			err = fmt.Errorf("plist 解析失败: %v", r)
		}
	}()
	format, err = hplist.Unmarshal(data, &value)
	if err != nil {
		return nil, format, fmt.Errorf("%splist 解析失败: %w", notPlistHint(data), err)
	}
	return value, format, nil
}

// notPlistHint 在文件根本不像 plist 时,先说这句再报库里的错。
//
// 库里对一个随手指错的文件报的是 "missing = in dictionary at line 0" ——
// 那是 OpenStep 文本格式解析器的话,调用方看了只会以为自己的 plist 坏了,
// 而实际情况是路径指到了别的文件上。
//
// 只加提示不直接拒绝:OpenStep / GNUStep 格式的 plist 开头确实什么标志都没有,
// 拦掉的话就把这两种合法格式一起拦了。
func notPlistHint(data []byte) string {
	if len(data) >= 6 && string(data[:6]) == "bplist" {
		return ""
	}
	// BOM 要单独剥:TrimSpace 不认它,留着的话一个带 BOM 的 XML plist
	// 会被误判成"不是 plist",在真正的错误前面多贴一句假提示
	head := strings.TrimSpace(strings.TrimPrefix(string(data[:min(len(data), 64)]), "\uFEFF"))
	for _, p := range []string{"<?xml", "<plist", "<!DOCTYPE plist", "{", "("} {
		if strings.HasPrefix(head, p) {
			return ""
		}
	}
	return "这个文件开头既不是 bplist 也不是 XML,多半根本不是 plist(确认下路径指对了没有);"
}
