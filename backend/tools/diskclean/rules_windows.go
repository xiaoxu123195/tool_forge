//go:build windows

package diskclean

import (
	"context"
	"os"
	"time"
)

// builtinRules Windows 上的缓存规则。
//
// 收录标准只有一条:删了之后程序会自己重新生成,用户除了"第一次打开慢一点"
// 感觉不到任何区别。所以这里只有缓存、临时文件、日志和转储 ——
// Cookie、历史记录、登录状态、聊天记录、会话这些一概不碰,那是隐私擦除,不是清理。
//
// 每条路径都对着真实的目录核过里面装的是什么。核不了的(机器上没装、文档也没写清楚的)
// 宁可不收:路径写错了只是这条不显示,可要是写对了路径、却没弄清里面是什么,删掉的就是用户的数据。
// 比如 WPS 的 office6 下面有个 backup,里面是文档的自动备份,和缓存挨着,所以 WPS 整个不收。
//
// 默认只勾系统那几项;其余的删了要重新下载、或者程序开着时多半删不动,都留给用户自己勾
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
			ID: "sys-delivery-opt", Group: "系统", Name: "传递优化缓存",
			Desc:    "Windows 更新和应用商店下载时留下的分发缓存。交给系统自己的清理命令来清,不直接删文件",
			Admin:   true,
			Measure: measureDeliveryOptimization,
			Clean:   cleanDeliveryOptimization,
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
			Measure: func(context.Context) (int64, int64, bool) {
				size, n, err := recycleBinInfo()
				return size, n, err == nil
			},
			Clean: func(context.Context) error { return emptyRecycleBin() },
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

		// ---- 常用软件 ----
		electronApp("app-discord", "常用软件", "Discord", `%APPDATA%\discord`, "Discord.exe"),
		electronApp("app-slack", "常用软件", "Slack", `%APPDATA%\Slack`, "slack.exe"),
		electronApp("app-postman", "常用软件", "Postman", `%APPDATA%\Postman`, "Postman.exe"),
		electronApp("app-notion", "常用软件", "Notion", `%APPDATA%\Notion`, "Notion.exe"),

		// ---- 国产软件 ----
		{
			ID: "cn-feishu", Group: "国产软件", Name: "飞书 缓存",
			Desc: "界面的代码缓存和显卡缓存。不碰聊天记录、文件和登录状态",
			Paths: []string{
				`%APPDATA%\LarkShell\CodeCache`,
				`%APPDATA%\LarkShell\ShaderCache`,
				`%APPDATA%\LarkShell\GrShaderCache`,
				`%APPDATA%\LarkShell\GraphiteDawnCache`,
			},
			Procs: []string{"Feishu.exe"},
		},
		electronApp("cn-baidunetdisk", "国产软件", "百度网盘", `%APPDATA%\baidunetdisk`, "BaiduNetdisk.exe"),
		{
			ID: "cn-cloudmusic", Group: "国产软件", Name: "网易云音乐 缓存",
			Desc: "边听边存的歌曲缓存和临时文件,删了再听时重新下载。自己下载的歌不在这里,不受影响",
			Paths: []string{
				`%LOCALAPPDATA%\NetEase\CloudMusic\Cache`,
				`%LOCALAPPDATA%\NetEase\CloudMusic\Temp`,
			},
			Procs: []string{"cloudmusic.exe"},
		},
		{
			ID: "cn-wemeet-logs", Group: "国产软件", Name: "腾讯会议 日志",
			Desc:  "运行日志。不碰会议记录、聊天和账号数据",
			Paths: []string{`%APPDATA%\Tencent\WeMeet\Global\Logs`},
			Procs: []string{"WeMeetApp.exe"},
		},

		// ---- 影音创作 ----
		{
			ID: "media-adobe", Group: "影音创作", Name: "Adobe 媒体缓存",
			Desc: "Premiere、After Effects 为了流畅预览生成的媒体缓存,删了下次打开工程会重新生成,要等一会儿",
			Paths: []string{
				`%APPDATA%\Adobe\Common\Media Cache Files`,
				`%APPDATA%\Adobe\Common\Media Cache`,
			},
			Procs: []string{"Adobe Premiere Pro.exe", "AfterFX.exe"},
		},
		{
			ID: "media-obs", Group: "影音创作", Name: "OBS 日志",
			Desc: "OBS 的运行日志和崩溃报告",
			Paths: []string{
				`%APPDATA%\obs-studio\logs`,
				`%APPDATA%\obs-studio\crashes`,
			},
			Procs: []string{"obs64.exe"},
		},

		// ---- 设计建模 ----
		{
			ID: "3d-unity", Group: "设计建模", Name: "Unity 全局缓存",
			Desc:  "Unity 编辑器下载的包和资源商店缓存,删了用到时重新下载",
			Paths: []string{`%LOCALAPPDATA%\Unity\cache`},
			Procs: []string{"Unity.exe"},
		},
		{
			ID: "3d-unreal", Group: "设计建模", Name: "虚幻引擎派生数据缓存",
			Desc:  "编译好的着色器等派生数据,删了下次打开项目要重新编译,可能要很久",
			Paths: []string{`%LOCALAPPDATA%\UnrealEngine\Common\DerivedDataCache`},
			Procs: []string{"UnrealEditor.exe"},
		},

		// ---- 游戏平台 ----
		{
			ID: "game-steam", Group: "游戏平台", Name: "Steam 网页缓存",
			Desc:  "Steam 客户端内置浏览器的缓存。不碰游戏、存档和账号",
			Paths: []string{`%LOCALAPPDATA%\Steam\htmlcache`},
			Procs: []string{"steam.exe"},
		},
		{
			ID: "game-epic", Group: "游戏平台", Name: "Epic 网页缓存",
			Desc:  "Epic 启动器内置浏览器的缓存。不碰游戏、存档和账号",
			Paths: []string{`%LOCALAPPDATA%\EpicGamesLauncher\Saved\webcache*`},
			Procs: []string{"EpicGamesLauncher.exe"},
		},

		// ---- 开发工具 ----
		{
			ID: "dev-npm", Group: "开发工具", Name: "npm 缓存",
			Desc:  "下载过的包,删了下次安装时重新下载",
			Paths: []string{`%LOCALAPPDATA%\npm-cache`},
		},
		{
			ID: "dev-pnpm", Group: "开发工具", Name: "pnpm 元数据缓存",
			Desc:  "包的元数据缓存,删了下次安装时重新拉取。不碰 pnpm 的包仓库",
			Paths: []string{`%LOCALAPPDATA%\pnpm-cache`},
		},
		{
			ID: "dev-yarn", Group: "开发工具", Name: "Yarn 缓存",
			Desc:  "下载过的包,删了下次安装时重新下载",
			Paths: []string{`%LOCALAPPDATA%\Yarn\Cache`},
		},
		{
			ID: "dev-node-build", Group: "开发工具", Name: "Node 构建下载缓存",
			Desc: "Electron、electron-builder、node-gyp 下载的二进制和头文件,删了下次构建时重新下载",
			Paths: []string{
				`%LOCALAPPDATA%\electron\Cache`,
				`%LOCALAPPDATA%\electron-builder\Cache`,
				`%LOCALAPPDATA%\node-gyp\Cache`,
			},
		},
		{
			ID: "dev-pip", Group: "开发工具", Name: "pip 缓存",
			Desc:  "下载过的 Python 包和编译好的 wheel,删了下次安装时重新下载",
			Paths: []string{`%LOCALAPPDATA%\pip\Cache`},
		},
		{
			ID: "dev-uv", Group: "开发工具", Name: "uv 缓存",
			Desc:  "uv 下载和解包过的 Python 包,删了下次安装时重新下载。不碰 uv 装的 Python 本身",
			Paths: []string{`%LOCALAPPDATA%\uv\cache`},
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
			ID: "dev-jetbrains", Group: "开发工具", Name: "JetBrains IDE 缓存、索引和日志",
			Desc: "IntelliJ IDEA、GoLand、PyCharm 等的缓存、索引和日志,删了下次打开项目要重新建索引",
			Paths: []string{
				`%LOCALAPPDATA%\JetBrains\*\caches`,
				`%LOCALAPPDATA%\JetBrains\*\index`,
				`%LOCALAPPDATA%\JetBrains\*\log`,
			},
		},
		{
			ID: "dev-android-studio", Group: "开发工具", Name: "Android Studio 缓存、索引和日志",
			Desc: "和 JetBrains 的一样,删了下次打开项目要重新建索引。不碰 SDK 和模拟器",
			Paths: []string{
				`%LOCALAPPDATA%\Google\AndroidStudio*\caches`,
				`%LOCALAPPDATA%\Google\AndroidStudio*\index`,
				`%LOCALAPPDATA%\Google\AndroidStudio*\log`,
			},
			Procs: []string{"studio64.exe"},
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

// electronApp 用 Electron / Chromium 内核做的桌面程序:缓存目录都是这几个固定的名字。
// 只清这几个 —— 登录状态、聊天记录、本地数据库都在同一层的别的目录里
func electronApp(id, group, name, userData, exe string) CacheRule {
	sub := func(s string) string { return userData + `\` + s }
	return CacheRule{
		ID: id, Group: group, Name: name + " 缓存",
		Desc: "界面的网页缓存、代码缓存和显卡缓存。不碰登录状态、聊天记录和本地数据",
		Paths: []string{
			sub("Cache"),
			sub("Code Cache"),
			sub("GPUCache"),
			sub("DawnCache"),
			sub("DawnGraphiteCache"),
			sub("DawnWebGPUCache"),
			sub("ShaderCache"),
			sub("GrShaderCache"),
			sub("GraphiteDawnCache"),
		},
		Procs: []string{exe},
	}
}

// deliveryOptimizationCache 传递优化服务的缓存目录
func deliveryOptimizationCache() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		return ""
	}
	return root + `\ServiceProfiles\NetworkService\AppData\Local\Microsoft\Windows\DeliveryOptimization\Cache`
}

func measureDeliveryOptimization(ctx context.Context) (int64, int64, bool) {
	dir := deliveryOptimizationCache()
	if dir == "" {
		return 0, 0, false
	}
	return measureDir(ctx, dir)
}
