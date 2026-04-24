package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

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
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
