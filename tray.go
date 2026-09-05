package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/energye/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"sm-gui/backend/winutil"
)

// appCtx 由 startup 注入，供托盘菜单调用 Wails runtime。
var appCtx context.Context

// trayQuitting 标记应用正在退出：托盘消息循环正常返回时不再重试。
var trayQuitting atomic.Bool

// trayReady 标记本次 systray.Run 的 onTrayReady 是否已触发。
var trayReady atomic.Bool

// trayLog 把托盘生命周期事件写入 data/tray.log（带时间戳），
// 用于排查自启动场景下托盘图标空白/无响应的问题。
func trayLog(format string, args ...interface{}) {
	f, err := os.OpenFile(filepath.Join(getDataDir(), "tray.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n",
		time.Now().Format("2006-01-02 15:04:05.000"),
		fmt.Sprintf(format, args...))
}

// setupTray 启动系统托盘。
// Windows 的窗口消息只投递到创建窗口的 OS 线程队列，因此必须在
// 同一个锁定线程上创建托盘窗口并泵送消息，否则点击事件永远收不到。
func setupTray(ctx context.Context) {
	appCtx = ctx
	go func() {
		runtime.LockOSThread()

		exe, _ := os.Executable()
		cwd, _ := os.Getwd()
		trayLog("setupTray: pid=%d admin=%v exe=%q cwd=%q args=%v",
			os.Getpid(), winutil.IsAdmin(), exe, cwd, os.Args[1:])

		// 开机自启动时程序可能比 explorer/任务栏先启动，此时 Shell_NotifyIcon
		// 注册失败 → 托盘图标空白且点击无响应。等任务栏就绪（最多 60 秒）
		// 再注册托盘；超时则照常注册，由库的 TaskbarCreated 处理兜底。
		t0 := time.Now()
		ok := winutil.WaitForTaskbar(60 * time.Second)
		trayLog("wait taskbar: ok=%v elapsed=%v", ok, time.Since(t0).Round(time.Millisecond))

		// 注册失败自愈：registerSystray 中任一 Win32 调用失败时，库只往
		// stderr 打一行日志就早退，nativeLoop 照样空转（图标空白、点击无响应、
		// ready 永不触发）。看门狗检测 ready 超时后向托盘线程投递 WM_QUIT，
		// 让 Run 返回；清理残留的窗口类后再重试注册
		//（登录会话初始化竞态，kolide/launcher#1241 同案例）。
		tid := winutil.CurrentThreadID()
		for attempt := 1; attempt <= 5; attempt++ {
			trayReady.Store(false)
			trayLog("systray.Run start (attempt %d)", attempt)
			watchdog := time.AfterFunc(10*time.Second, func() {
				if trayReady.Load() || trayQuitting.Load() {
					return
				}
				trayLog("ready 超时（10s 未触发 onTrayReady），投递 WM_QUIT 重试")
				if err := winutil.PostThreadQuit(tid); err != nil {
					trayLog("PostThreadQuit 失败: %v", err)
				}
			})
			systray.Run(onTrayReady, onTrayExit)
			watchdog.Stop()
			trayLog("systray.Run returned (quitting=%v)", trayQuitting.Load())
			if trayQuitting.Load() {
				return
			}
			// 清理残留的窗口类/隐藏窗口，否则重试必然报 "Class already exists"
			if ok, err := winutil.CleanupTrayClass(); !ok {
				trayLog("CleanupTrayClass 失败: %v", err)
			}
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		trayLog("托盘重试次数用尽，放弃")
	}()
}

// stopTray 移除托盘图标（在应用退出时调用）。
func stopTray() {
	trayQuitting.Store(true)
	systray.Quit()
}

func onTrayReady() {
	trayReady.Store(true)
	trayLog("onTrayReady: 注册图标/菜单")
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
	trayLog("onTrayExit")
}

// ─── 图标生成 ─────────────────────────────────────────────────────────────────────
// 项目内没有图标资源，这里程序化生成一个 32x32 图标：
// 紫色圆角方块 + 白色圆点。
// 注意：必须生成经典 BMP 像素格式的 ICO（BITMAPINFOHEADER + BGRA XOR + AND mask），
// 不能用 PNG 包装的 ICO——energye/systray 经 LoadImageW(LR_LOADFROMFILE) 加载，
// 而 LoadImageW 不支持 PNG 压缩的 ICO 条目（加载失败 → 托盘图标空白）。

func trayIconBytes() []byte {
	const size = 32
	px := genIconBGRA(size)

	// AND mask 行跨度：每行位数向上取整到 32 位（字节对齐）
	andStride := ((size + 31) / 32) * 4
	xorLen := size * size * 4
	andLen := andStride * size

	buf := make([]byte, 0, 6+16+40+xorLen+andLen)
	// ICONDIR：保留 / 类型=图标 / 数量=1
	buf = append(buf, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00)
	// ICONDIRENTRY：宽 / 高 / 调色板 / 保留 / 颜色平面 / 位深
	buf = append(buf, byte(size), byte(size), 0x00, 0x00, 0x01, 0x00, 0x20, 0x00)
	dataLen := uint32(40 + xorLen + andLen)
	buf = append(buf, byte(dataLen), byte(dataLen>>8), byte(dataLen>>16), byte(dataLen>>24))
	buf = append(buf, 0x16, 0x00, 0x00, 0x00) // 数据偏移 = 22
	// BITMAPINFOHEADER：biHeight = 高×2（XOR + AND 两段）
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)
	binary.LittleEndian.PutUint32(hdr[4:], size)
	binary.LittleEndian.PutUint32(hdr[8:], size*2)
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], 32)
	binary.LittleEndian.PutUint32(hdr[20:], uint32(xorLen+andLen))
	buf = append(buf, hdr...)
	// XOR 像素：自下而上，BGRA（genIconBGRA 已按 B,G,R,A 顺序输出每像素）
	for y := size - 1; y >= 0; y-- {
		row := px[y*size*4 : (y+1)*size*4]
		buf = append(buf, row...)
	}
	// AND mask：全 0（32bpp 透明度由 alpha 通道表达）
	buf = append(buf, make([]byte, andLen)...)
	return buf
}

// genIconBGRA 生成 32bpp BGRA 像素（自上而下，每像素 4 字节，按 B,G,R,A 排列）。
func genIconBGRA(size int) []byte {
	const radius = 8
	const dotR = 7

	px := make([]byte, size*size*4)
	set := func(x, y int, c color.RGBA) {
		i := (y*size + x) * 4
		px[i] = c.B
		px[i+1] = c.G
		px[i+2] = c.R
		px[i+3] = c.A
	}

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
			set(x, y, bg)
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
				set(x, y, dot)
			case d2 <= 4*4:
				set(x, y, inner)
			}
		}
	}
	return px
}
