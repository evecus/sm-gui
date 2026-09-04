package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"runtime"

	"github.com/energye/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// appCtx 由 startup 注入，供托盘菜单调用 Wails runtime。
var appCtx context.Context

// setupTray 启动系统托盘。
// Windows 的窗口消息只投递到创建窗口的 OS 线程队列，因此必须在
// 同一个锁定线程上创建托盘窗口并泵送消息，否则点击事件永远收不到。
func setupTray(ctx context.Context) {
	appCtx = ctx
	go func() {
		runtime.LockOSThread()
		systray.Run(onTrayReady, onTrayExit)
	}()
}

// stopTray 移除托盘图标（在应用退出时调用）。
func stopTray() {
	systray.Quit()
}

func onTrayReady() {
	systray.SetIcon(trayIconBytes())
	systray.SetTooltip("SM GUI")

	mShow := systray.AddMenuItem("显示主窗口", "打开主界面")
	mShow.Click(func() {
		wruntime.WindowUnminimise(appCtx)
		wruntime.WindowShow(appCtx)
	})

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("退出", "退出程序（自动还原系统代理并停止核心）")
	mQuit.Click(func() {
		// 直接退出 Wails 应用，OnShutdown 会停止核心并还原系统代理
		wruntime.Quit(appCtx)
	})

	// 左键点击托盘图标显示主窗口（右键弹出菜单由库处理）
	systray.SetOnClick(func(_ systray.IMenu) {
		wruntime.WindowUnminimise(appCtx)
		wruntime.WindowShow(appCtx)
	})
}

func onTrayExit() {
	// 托盘循环已结束，无需清理
}

// ─── 图标生成 ─────────────────────────────────────────────────────────────────────
// 项目内没有图标资源，这里程序化生成一个 32x32 图标：
// 紫色圆角方块 + 白色圆点，PNG 编码后包一层 ICO 容器（Windows 托盘要求 ICO 格式）。

func trayIconBytes() []byte {
	pngBytes, err := genIconPNG()
	if err != nil {
		return nil
	}
	icoBytes, err := pngToICO(pngBytes)
	if err != nil {
		return nil
	}
	return icoBytes
}

func genIconPNG() ([]byte, error) {
	const size = 32
	const radius = 8
	const dotR = 7

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{R: 0x6C, G: 0x5C, B: 0xE7, A: 0xFF}    // 紫色 #6C5CE7
	dot := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}   // 白色
	inner := color.RGBA{R: 0x6C, G: 0x5C, B: 0xE7, A: 0xFF} // 中心点同底色

	r2 := radius * radius
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// 圆角判定
			cx, cy := x, y
			if cx > size-1-radius {
				cx = size - 1 - cx
			}
			if cy > size-1-radius {
				cy = size - 1 - cy
			}
			if cx < radius && cy < radius {
				dx, dy := radius-cx, radius-cy
				if dx*dx+dy*dy > r2 {
					continue // 透明角
				}
			}
			img.Set(x, y, bg)
		}
	}

	// 中心白色圆 + 内部同色点，形成环形
	c := size / 2
	for y := c - dotR; y <= c+dotR; y++ {
		for x := c - dotR; x <= c+dotR; x++ {
			dx, dy := x-c, y-c
			d2 := dx*dx + dy*dy
			switch {
			case d2 <= dotR*dotR && d2 > 4*4:
				img.Set(x, y, dot)
			case d2 <= 4*4:
				img.Set(x, y, inner)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pngToICO 把单张 PNG 包装成 ICO 容器（ICO 目录项允许内嵌 PNG 数据，Vista+ 支持）。
func pngToICO(pngBytes []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	var buf bytes.Buffer
	// ICONDIR
	buf.Write([]byte{0x00, 0x00}) // 保留
	buf.Write([]byte{0x01, 0x00}) // 类型: 图标
	buf.Write([]byte{0x01, 0x00}) // 数量: 1
	// ICONDIRENTRY
	buf.WriteByte(byte(w % 256))  // 宽（256 写 0）
	buf.WriteByte(byte(h % 256))  // 高
	buf.WriteByte(0x00)           // 调色板数
	buf.WriteByte(0x00)           // 保留
	buf.Write([]byte{0x01, 0x00}) // 颜色平面
	buf.Write([]byte{0x20, 0x00}) // 位深 32
	size := uint32(len(pngBytes))
	buf.WriteByte(byte(size))
	buf.WriteByte(byte(size >> 8))
	buf.WriteByte(byte(size >> 16))
	buf.WriteByte(byte(size >> 24))
	buf.Write([]byte{0x16, 0x00, 0x00, 0x00}) // 数据偏移 = 22
	// PNG 数据
	buf.Write(pngBytes)
	return buf.Bytes(), nil
}
