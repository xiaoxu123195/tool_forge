//go:build windows

package diskclean

import "time"

// builtinRules Windows 上的缓存规则。
//
// 收录标准只有一条:删了之后程序会自己重新生成,用户除了"第一次打开慢一点"
// 感觉不到任何区别。所以这里只有缓存、临时文件、日志和转储 ——
// Cookie、历史记录、登录状态、会话记录这些一概不碰,那是隐私擦除,不是清理。
//
// 默认只勾系统那几项;开发工具的缓存删了要重新下载,浏览器开着时多半删不动,
// 都留给用户自己勾
func builtinRules() []CacheRule {
	const day = 24 * time.Hour
	return []CacheRule{
		// ---- 系统 ----
		{
			ID: "sys-user-temp", Group: "系统", Name: "用户临时文件",
			Desc:  "程序运行时随手丢下的临时文件。只删一天以前的——正在安装的程序可能还在往里写",
			Paths: []string{`%TEMP%`}, MinAge: day, Default: true,
		},
		{
			ID: "sys-win-temp", Group: "系统", Name: "系统临时文件",
			Desc:  "系统和后台服务留下的临时文件,同样只删一天以前的",
			Paths: []string{`%SystemRoot%\Temp`}, MinAge: day, Admin: true, Default: true,
		},
		{
			ID: "sys-crash-dumps", Group: "系统", Name: "程序崩溃转储",
			Desc:  "程序崩溃时留下的内存快照,只有排查那一次崩溃时才用得上",
			Paths: []string{`%LOCALAPPDATA%\CrashDumps`}, Default: true,
		},
		{
			ID: "sys-wer", Group: "系统", Name: "错误报告",
			Desc: "Windows 错误报告的存档和待发送队列",
			Paths: []string{
				`%LOCALAPPDATA%\Microsoft\Windows\WER\ReportArchive`,
				`%LOCALAPPDATA%\Microsoft\Windows\WER\ReportQueue`,
			},
			Default: true,
		},
		{
			ID: "sys-wer-all", Group: "系统", Name: "错误报告(所有用户)",
			Desc: "系统级的错误报告存档、队列和临时文件",
			Paths: []string{
				`%ProgramData%\Microsoft\Windows\WER\ReportArchive`,
				`%ProgramData%\Microsoft\Windows\WER\ReportQueue`,
				`%ProgramData%\Microsoft\Windows\WER\Temp`,
			},
			Admin: true, Default: true,
		},
		{
			ID: "sys-inetcache", Group: "系统", Name: "Internet 临时文件",
			Desc:  "系统网页组件的缓存,老版 IE 和一些内嵌网页的程序在用",
			Paths: []string{`%LOCALAPPDATA%\Microsoft\Windows\INetCache`}, Default: true,
		},
		{
			ID: "sys-thumbcache", Group: "系统", Name: "缩略图缓存",
			Desc:  "资源管理器的图片、视频缩略图,删了会重新生成。资源管理器开着时大部分被占用,会跳过",
			Paths: []string{`%LOCALAPPDATA%\Microsoft\Windows\Explorer`}, Match: []string{"thumbcache_*.db"},
		},
		{
			ID: "sys-shader-cache", Group: "系统", Name: "显卡着色器缓存",
			Desc: "DirectX 和显卡驱动编译好的着色器,删了第一次打开游戏或 3D 程序会慢一点",
			Paths: []string{
				`%LOCALAPPDATA%\D3DSCache`,
				`%LOCALAPPDATA%\NVIDIA\DXCache`,
				`%LOCALAPPDATA%\NVIDIA\GLCache`,
				`%LOCALAPPDATA%\AMD\DxCache`,
			},
		},
		{
			ID: "sys-wu-download", Group: "系统", Name: "Windows 更新下载缓存",
			Desc:  "已经装好的更新留下的安装包。正在下载或安装更新时不要清",
			Paths: []string{`%SystemRoot%\SoftwareDistribution\Download`}, Admin: true,
		},
		{
			ID: "sys-cbs-logs", Group: "系统", Name: "组件服务日志",
			Desc:  "系统组件安装、更新的日志,出过问题时能涨到几个 GB。只删一天以前的",
			Paths: []string{`%SystemRoot%\Logs\CBS`}, Match: []string{"*.log", "*.cab"},
			MinAge: day, Admin: true,
		},
		{
			ID: "sys-minidump", Group: "系统", Name: "蓝屏小转储",
			Desc:  "蓝屏时留下的转储文件。查蓝屏原因要用,没在查就可以清",
			Paths: []string{`%SystemRoot%\Minidump`}, Admin: true,
		},
		{
			ID: "sys-memory-dmp", Group: "系统", Name: "蓝屏完整内存转储",
			Desc:  "最近一次蓝屏的完整内存转储(MEMORY.DMP),往往有好几个 GB",
			Files: []string{`%SystemRoot%\MEMORY.DMP`}, Admin: true,
		},
		{
			ID: "sys-recycle-bin", Group: "系统", Name: "回收站",
			Desc:       "清空所有盘的回收站。回收站是删错时的后悔药,确定不要了再清",
			RecycleBin: true,
		},

		// ---- 浏览器 ----
		chromium("chrome", "Chrome", `%LOCALAPPDATA%\Google\Chrome\User Data`, "chrome.exe"),
		chromium("edge", "Edge", `%LOCALAPPDATA%\Microsoft\Edge\User Data`, "msedge.exe"),
		chromium("brave", "Brave", `%LOCALAPPDATA%\BraveSoftware\Brave-Browser\User Data`, "brave.exe"),
		{
			ID: "browser-firefox", Group: "浏览器", Name: "Firefox 缓存",
			Desc: "网页缓存、启动缓存和缩略图。不碰 Cookie、历史记录、密码和登录状态",
			Paths: []string{
				`%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\cache2`,
				`%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\startupCache`,
				`%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\thumbnails`,
			},
			Procs: []string{"firefox.exe"},
		},

		// ---- 开发工具 ----
		{
			ID: "dev-npm", Group: "开发工具", Name: "npm 缓存",
			Desc:  "下载过的包,删了下次安装时重新下载",
			Paths: []string{`%LOCALAPPDATA%\npm-cache`},
		},
		{
			ID: "dev-yarn", Group: "开发工具", Name: "Yarn 缓存",
			Desc:  "下载过的包,删了下次安装时重新下载",
			Paths: []string{`%LOCALAPPDATA%\Yarn\Cache`},
		},
		{
			ID: "dev-pip", Group: "开发工具", Name: "pip 缓存",
			Desc:  "下载过的 Python 包和编译好的 wheel,删了下次安装时重新下载",
			Paths: []string{`%LOCALAPPDATA%\pip\Cache`},
		},
		{
			ID: "dev-go-build", Group: "开发工具", Name: "Go 编译缓存",
			Desc:  "go build 的中间产物,删了下次编译会慢一些。正在编译时不要清",
			Paths: []string{`%LOCALAPPDATA%\go-build`},
		},
		{
			ID: "dev-gradle", Group: "开发工具", Name: "Gradle 缓存",
			Desc:  "依赖和构建缓存,通常很大。先停掉守护进程(gradlew --stop)再清,删了下次构建重新下载",
			Paths: []string{`%USERPROFILE%\.gradle\caches`},
		},
		{
			ID: "dev-nuget", Group: "开发工具", Name: "NuGet 下载缓存",
			Desc: "NuGet 的 HTTP 缓存,不是已经装进项目的包",
			Paths: []string{
				`%LOCALAPPDATA%\NuGet\v3-cache`,
				`%LOCALAPPDATA%\NuGet\plugins-cache`,
			},
		},
		{
			ID: "dev-cargo", Group: "开发工具", Name: "Cargo 下载缓存",
			Desc:  "下载过的 crate 压缩包,删了用到时重新下载",
			Paths: []string{`%USERPROFILE%\.cargo\registry\cache`},
		},
		{
			ID: "dev-vscode", Group: "开发工具", Name: "VS Code 缓存和日志",
			Desc: "界面缓存、旧版本留下的编译缓存和日志。不碰设置、扩展和工作区数据",
			Paths: []string{
				`%APPDATA%\Code\Cache`,
				`%APPDATA%\Code\CachedData`,
				`%APPDATA%\Code\Code Cache`,
				`%APPDATA%\Code\GPUCache`,
				`%APPDATA%\Code\CachedExtensionVSIXs`,
				`%APPDATA%\Code\logs`,
			},
			Procs: []string{"Code.exe"},
		},
		{
			ID: "dev-jetbrains", Group: "开发工具", Name: "JetBrains IDE 缓存和日志",
			Desc: "IntelliJ IDEA、GoLand、PyCharm 等的缓存和日志,删了下次打开项目要重新建索引",
			Paths: []string{
				`%LOCALAPPDATA%\JetBrains\*\caches`,
				`%LOCALAPPDATA%\JetBrains\*\log`,
			},
		},
	}
}

// chromium Chrome 系浏览器的缓存长一个样,只是装在不同的地方。
// 只清"缓存"那几个目录 —— Cookie、历史记录、密码、登录状态、扩展数据都在别的目录里
func chromium(id, name, userData, exe string) CacheRule {
	profile := func(sub string) string { return userData + `\*\` + sub }
	return CacheRule{
		ID: "browser-" + id, Group: "浏览器", Name: name + " 缓存",
		Desc: "网页缓存、代码缓存和显卡缓存。不碰 Cookie、历史记录、密码和登录状态",
		Paths: []string{
			profile("Cache"),
			profile("Code Cache"),
			profile("GPUCache"),
			profile("DawnCache"),
			profile("DawnGraphiteCache"),
			profile("DawnWebGPUCache"),
			userData + `\ShaderCache`,
			userData + `\GrShaderCache`,
			userData + `\GraphiteDawnCache`,
		},
		Procs: []string{exe},
	}
}
