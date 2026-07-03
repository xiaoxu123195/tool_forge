package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailswin "github.com/wailsapp/wails/v2/pkg/options/windows"

	"tool_forge/backend/updater"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 自搬家钩子:若当前 exe 在 Downloads,把自己拷到注册表登记的安装路径再重启。
	// 非该场景 → 仅刷新注册表里的安装路径,正常继续启动。
	updater.HandleStartup()

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "Tool Forge",
		Width:     1200,
		Height:    800,
		Frameless: true,
		// 先隐藏窗口,等前端首帧画好后由前端 WindowShow() 显示;
		// 否则原生窗口会先于 webview 出现,露出透明/Mica 黑底 → 启动黑屏闪一下
		StartHidden: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// 透明窗口底色:Glass 主题需要 webview 透到 Mica 层;
		// 其他主题靠 body 的 bg-background 覆盖,看不到底层
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		Windows: &wailswin.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
			BackdropType:         wailswin.Mica,
		},
		// 启用原生文件拖放:拖入文件可拿到绝对路径(文件哈希工具流式读取用)。
		// 仅对带 --wails-drop-target:drop 的元素生效,不影响其它工具的 HTML5 图片拖拽。
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop: true,
		},
		OnStartup:  app.startup,
		OnDomReady: app.domready,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
