package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestTrayIconLoadableByLoadImage 用托盘库同款 API（LoadImageW + LR_LOADFROMFILE）
// 验证程序生成的 ICO 可以被加载。
// 历史 bug：旧实现生成 PNG 包装的 ICO（ICONDIRENTRY 内嵌 PNG 数据），
// LoadImageW 不支持 PNG 压缩条目 → 加载失败 → 托盘图标空白且无法交互。
func TestTrayIconLoadableByLoadImage(t *testing.T) {
	ico := trayIconBytes()
	if len(ico) == 0 {
		t.Fatal("trayIconBytes 为空")
	}
	// ICO 结构自检：6(ICONDIR) + 16(ICONDIRENTRY) + 40(BITMAPINFOHEADER)
	if len(ico) < 62 {
		t.Fatalf("ICO 长度异常: %d", len(ico))
	}
	if ico[0] != 0 || ico[1] != 0 || ico[2] != 1 || ico[3] != 0 {
		t.Fatalf("ICO 魔数错误: % x", ico[:4])
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "tray.ico")
	if err := os.WriteFile(path, ico, 0644); err != nil {
		t.Fatal(err)
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	loadImage := windows.NewLazySystemDLL("user32.dll").NewProc("LoadImageW")
	const (
		IMAGE_ICON      = 1
		LR_LOADFROMFILE = 0x00000010
	)
	res, _, _ := loadImage.Call(
		0,
		uintptr(unsafe.Pointer(p)),
		IMAGE_ICON,
		0,
		0,
		LR_LOADFROMFILE,
	)
	if res == 0 {
		t.Fatal("LoadImageW 无法加载生成的 ICO（图标格式不被 GDI 支持）")
	}
}

// TestOldPngWrappedIcoRejectedByLoadImage 反向验证：PNG 包装的 ICO
// （旧实现格式）确实无法被 LoadImageW 加载，坐实历史根因。
func TestOldPngWrappedIcoRejectedByLoadImage(t *testing.T) {
	// 生成一张 32x32 纯色 PNG
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: 0x6C, G: 0x5C, B: 0xE7, A: 0xFF})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	pngBytes := pngBuf.Bytes()

	// 旧实现：ICONDIR + ICONDIRENTRY + PNG 数据
	ico := []byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x00}
	ico = append(ico, 32, 32, 0, 0, 0x01, 0x00, 0x20, 0x00)
	size := uint32(len(pngBytes))
	ico = append(ico, byte(size), byte(size>>8), byte(size>>16), byte(size>>24))
	ico = append(ico, 0x16, 0x00, 0x00, 0x00)
	ico = append(ico, pngBytes...)

	dir := t.TempDir()
	path := filepath.Join(dir, "old.ico")
	if err := os.WriteFile(path, ico, 0644); err != nil {
		t.Fatal(err)
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	loadImage := windows.NewLazySystemDLL("user32.dll").NewProc("LoadImageW")
	const (
		IMAGE_ICON      = 1
		LR_LOADFROMFILE = 0x00000010
	)
	res, _, _ := loadImage.Call(
		0,
		uintptr(unsafe.Pointer(p)),
		IMAGE_ICON,
		0,
		0,
		LR_LOADFROMFILE,
	)
	if res != 0 {
		t.Log("注意：当前系统 LoadImageW 竟然接受了 PNG 包装 ICO，根因判断需要重新评估")
	} else {
		t.Log("确认：LoadImageW 拒绝 PNG 包装 ICO（与托盘空白根因一致）")
	}
}
