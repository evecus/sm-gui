package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:         "SM GUI",
		Width:         900,
		Height:        650,
		MinWidth:      750,
		MinHeight:     500,
		DisableResize: false,
		Fullscreen:    false,
		Frameless:     false,
		StartHidden:   false,
		// 点击窗口关闭按钮时仅隐藏窗口（最小化到托盘），程序继续在后台运行
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			IsZoomControlEnabled: false,
			DisablePinchZoom:     true,
			Theme:                windows.Light,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
