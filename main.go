package main

import (
	"embed"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// GUI 子系统下 stderr 不可见：systray 等库的失败只走标准 log，
	// 重定向到 data/tray.log 才能拿到托盘注册失败的具体原因。
	if f, err := os.OpenFile(filepath.Join(getDataDir(), "tray.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		defer f.Close()
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		log.SetOutput(io.MultiWriter(f, os.Stderr))
	}

	app := NewApp()

	// 开机自启动（静默模式）带 --silent 参数：不显示主窗口，仅托盘图标。
	// 手动启动不带参数，正常显示窗口。
	silent := false
	for _, arg := range os.Args[1:] {
		if arg == "--silent" {
			silent = true
			break
		}
	}

	err := wails.Run(&options.App{
		Title:         "SM GUI",
		Width:         900,
		Height:        650,
		MinWidth:      750,
		MinHeight:     500,
		DisableResize: false,
		Fullscreen:    false,
		Frameless:     false,
		StartHidden:   silent,
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
			// 跟随系统浅色/深色模式
			Theme: windows.SystemDefault,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
