//go:build windows

package system

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 2x2:左上不透明红、右上全透明、左下半透明蓝、右下不透明绿
func sampleImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	img.Set(1, 0, color.NRGBA{})
	img.Set(0, 1, color.NRGBA{B: 255, A: 128})
	img.Set(1, 1, color.NRGBA{G: 255, A: 255})
	return img
}

func TestDIBOnWhite(t *testing.T) {
	dib := dibOnWhite(sampleImage())
	if len(dib) != 40+2*2*4 {
		t.Fatalf("长度 %d", len(dib))
	}
	le := binary.LittleEndian
	if le.Uint32(dib[0:]) != 40 || le.Uint32(dib[4:]) != 2 || le.Uint32(dib[8:]) != 2 ||
		le.Uint16(dib[12:]) != 1 || le.Uint16(dib[14:]) != 32 || le.Uint32(dib[16:]) != 0 {
		t.Fatalf("文件头不对: % x", dib[:40])
	}
	px := dib[40:]
	at := func(row, col int) [4]byte {
		i := (row*2 + col) * 4
		return [4]byte{px[i], px[i+1], px[i+2], px[i+3]} // B G R A
	}
	// 从最后一行往上排:内存里的第一行是图片的下面那行
	if got := at(1, 0); got != [4]byte{0, 0, 255, 255} {
		t.Errorf("红色不对: %v", got)
	}
	// 全透明的地方垫成白的,不能是黑的
	if got := at(1, 1); got != [4]byte{255, 255, 255, 255} {
		t.Errorf("透明处应该是白底: %v", got)
	}
	if got := at(0, 1); got != [4]byte{0, 255, 0, 255} {
		t.Errorf("绿色不对: %v", got)
	}
	// 半透明蓝叠在白底上:蓝满,红绿各剩一半左右
	if got := at(0, 0); got[0] != 255 || got[1] < 120 || got[1] > 135 || got[2] < 120 || got[2] > 135 || got[3] != 255 {
		t.Errorf("半透明处应该和白底混合: %v", got)
	}
}

// 真往系统剪贴板里写一次,再读回来核对。会覆盖剪贴板,默认不跑
func TestCopyImageRealClipboard(t *testing.T) {
	if os.Getenv("TOOLFORGE_CLIPBOARD_TEST") != "1" {
		t.Skip("会覆盖系统剪贴板;设 TOOLFORGE_CLIPBOARD_TEST=1 才跑")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, sampleImage()); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if err := CopyImage(data); err != nil {
		t.Fatal(err)
	}

	isAvailable := user32.NewProc("IsClipboardFormatAvailable")
	getData := user32.NewProc("GetClipboardData")
	globalSize := kernel32.NewProc("GlobalSize")

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(); err != nil {
		t.Fatal(err)
	}
	defer procCloseClipboard.Call()

	name, _ := windows.UTF16PtrFromString("PNG")
	pngFmt, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	// CF_DIBV5(17)和 CF_BITMAP(2)是系统按 CF_DIB 现场转出来的,认它们的程序也能粘
	for _, f := range []uintptr{pngFmt, cfDIB, 17, 2} {
		if r, _, _ := isAvailable.Call(f); r == 0 {
			t.Errorf("剪贴板上没有格式 %d", f)
		}
	}
	h, _, err := getData.Call(pngFmt)
	if h == 0 {
		t.Fatalf("读不出 PNG: %v", err)
	}
	size, _, _ := globalSize.Call(h)
	p, _, _ := procGlobalLock.Call(h)
	got := make([]byte, size)
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&got[0])), p, size)
	procGlobalUnlock.Call(h)
	// 系统分配的块可能比要的大一点,只比前面那段
	if len(got) < len(data) || !bytes.Equal(got[:len(data)], data) {
		t.Fatalf("PNG 那份不是原样放进去的")
	}
}
