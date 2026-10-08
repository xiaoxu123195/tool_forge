//go:build windows

package system

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procCloseClipboard           = user32.NewProc("CloseClipboard")
	procEmptyClipboard           = user32.NewProc("EmptyClipboard")
	procSetClipboardData         = user32.NewProc("SetClipboardData")
	procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	procGlobalAlloc              = kernel32.NewProc("GlobalAlloc")
	procGlobalLock               = kernel32.NewProc("GlobalLock")
	procGlobalUnlock             = kernel32.NewProc("GlobalUnlock")
	procGlobalFree               = kernel32.NewProc("GlobalFree")
	procRtlMoveMemory            = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfDIB        = 8
	gmemMoveable = 0x0002
)

// CopyImage 把一张 PNG 放进系统剪贴板,一次放两种格式:
//   - "PNG":Office、WPS、浏览器认这个,透明的地方能保住
//   - 位图(CF_DIB):给只认位图的程序。多数程序不理会位图里的透明通道,
//     透明的地方会变成黑色,所以这一份先垫上白底
func CopyImage(pngData []byte) error {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return fmt.Errorf("不是 PNG: %w", err)
	}
	dib := dibOnWhite(img)
	pngFormat, err := windows.UTF16PtrFromString("PNG")
	if err != nil {
		return err
	}

	// 剪贴板在哪个线程打开,就得在哪个线程关
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("清空剪贴板失败: %w", err)
	}
	f, _, err := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(pngFormat)))
	if f == 0 {
		return fmt.Errorf("注册剪贴板格式失败: %w", err)
	}
	if err := setClipboardData(f, pngData); err != nil {
		return err
	}
	return setClipboardData(cfDIB, dib)
}

// openClipboard 剪贴板同一时刻只能有一个程序打开,
// 别的程序(剪贴板管理器之类)刚好占着时稍等再试
func openClipboard() error {
	var last error
	for i := 0; i < 10; i++ {
		r, _, err := procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		last = err
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("剪贴板被别的程序占着: %w", last)
}

// setClipboardData 把一块数据交给剪贴板。交成功之后这块内存归系统管,不能再释放
func setClipboardData(format uintptr, data []byte) error {
	if len(data) == 0 {
		return errors.New("没有数据可写")
	}
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return fmt.Errorf("分配内存失败: %w", err)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("锁定内存失败: %w", err)
	}
	procRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(format, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("写剪贴板失败: %w", err)
	}
	return nil
}

// dibOnWhite 垫上白底,编成 32 位、不压缩的 DIB:
// 一个 BITMAPINFOHEADER,后面跟着从最后一行往上排的 BGRA 像素
func dibOnWhite(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	flat := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, b.Min, draw.Over)

	const headerSize = 40
	out := make([]byte, headerSize+w*h*4)
	le := binary.LittleEndian
	le.PutUint32(out[0:], headerSize)
	le.PutUint32(out[4:], uint32(w))
	le.PutUint32(out[8:], uint32(h)) // 正数:像素从最后一行往上排
	le.PutUint16(out[12:], 1)        // 位面数
	le.PutUint16(out[14:], 32)       // 每像素位数;压缩方式留 0,即不压缩
	le.PutUint32(out[20:], uint32(w*h*4))
	px := out[headerSize:]
	for y := 0; y < h; y++ {
		src := flat.Pix[(h-1-y)*flat.Stride:]
		row := px[y*w*4:]
		for x := 0; x < w; x++ {
			row[x*4+0] = src[x*4+2]
			row[x*4+1] = src[x*4+1]
			row[x*4+2] = src[x*4+0]
			row[x*4+3] = 0xff
		}
	}
	return out
}
