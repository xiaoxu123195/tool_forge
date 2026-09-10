package forensic

import (
	"fmt"
	"strings"
)

// 参数是前端和 MCP 那头一起用的接口,形状固定:
//
//	<platform> export [-k 关键词]... [-s 路径]... [-o 输出目录] [其它平台专有的]
//
// 换成原生实现之后仍然收这套参数,而不是换成结构体入参:MCP 工具的入参
// 已经按这个形状定下来了,agent 那边也是照着它给的。为了内部实现改一次
// 就动对外接口不值当 —— 何况 iOS 那条还在走命令行,两边得说同一种话。

// exportOptions 一次导出要的东西
type exportOptions struct {
	platform string
	keywords []string
	paths    []string
	output   string
	deviceID string
	// adbPath 仅 Android;空 = 自己找
	adbPath string
}

// nativeSupported 这套参数能不能用原生实现跑。
//
// 目前只有安卓的 export。别的(iOS 的 export / proxy / device list)仍然
// 交给命令行 —— 认不出来就老老实实回落,而不是猜着执行
func nativeSupported(opt exportOptions, ok bool) bool {
	return ok && opt.platform == "android"
}

// parseExportArgs 把 CLI 形状的参数解回结构。
// 第二个返回值为假表示"这不是一条我们认识的导出命令",调用方应该回落到命令行
func parseExportArgs(args []string) (exportOptions, bool) {
	var opt exportOptions
	if len(args) < 2 {
		return opt, false
	}
	opt.platform = args[0]
	if args[1] != "export" {
		return opt, false
	}
	if opt.platform != "android" && opt.platform != "ios" {
		return opt, false
	}

	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		// 只认这几个;出现别的 flag 说明调用方用了我们没实现的功能,
		// 这时候必须回落到命令行,而不是把它悄悄忽略掉
		var target *[]string
		var single *string
		switch {
		case a == "-k" || a == "--keyword":
			target = &opt.keywords
		case a == "-s" || a == "--specify-path":
			target = &opt.paths
		case a == "-o" || a == "--output":
			single = &opt.output
		case a == "-d" || a == "--device-id":
			single = &opt.deviceID
		default:
			return opt, false
		}
		if i+1 >= len(rest) {
			return opt, false
		}
		i++
		if target != nil {
			*target = append(*target, rest[i])
		} else {
			*single = rest[i]
		}
	}
	if strings.TrimSpace(opt.output) == "" {
		return opt, false
	}
	return opt, true
}

// describe 给日志开头用的一句话,说清楚这次要干什么
func (o exportOptions) describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s export → %s", o.platform, o.output)
	if len(o.paths) > 0 {
		fmt.Fprintf(&b, " (%d path)", len(o.paths))
	}
	if len(o.keywords) > 0 {
		fmt.Fprintf(&b, " (keywords: %s)", strings.Join(o.keywords, ", "))
	}
	return b.String()
}
