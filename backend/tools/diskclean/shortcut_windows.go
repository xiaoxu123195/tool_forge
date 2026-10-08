//go:build windows

package diskclean

import (
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procSHGetPathFromIDListEx = shell32.NewProc("SHGetPathFromIDListEx")

// pathFromIDList 把快捷方式里的项目标识列表交给系统转成文件路径。
// 只做转换,不解析也不"修复"快捷方式;控制面板、应用商店应用这类不是文件的项目转不出来,返回空
func pathFromIDList(idl []byte) string {
	if !validIDList(idl) {
		return ""
	}
	buf := make([]uint16, 32768)
	var ok uintptr
	shellCall(func() {
		ok, _, _ = procSHGetPathFromIDListEx.Call(
			uintptr(unsafe.Pointer(&idl[0])), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	})
	runtime.KeepAlive(idl)
	if ok == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// localFixed 路径在不在本机的固定盘上(不是网络路径、U 盘、光盘,也不是没接上的盘)
func localFixed(p string) bool {
	vol := filepath.VolumeName(p)
	if len(vol) != 2 || vol[1] != ':' {
		return false // \\server\share 这种
	}
	u, err := windows.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return false
	}
	return windows.GetDriveType(u) == windows.DRIVE_FIXED
}

// shortcutPlaces 要扫的位置。用系统的"已知文件夹"去取,不自己拼路径:
// 桌面可能被挪进 OneDrive,开始菜单也可能被改过位置
func shortcutPlaces() []shortcutPlace {
	want := []struct {
		name string
		id   *windows.KNOWNFOLDERID
		deep bool
	}{
		{"桌面", windows.FOLDERID_Desktop, false},
		{"公用桌面", windows.FOLDERID_PublicDesktop, false},
		{"开始菜单", windows.FOLDERID_StartMenu, true},
		{"开始菜单(所有用户)", windows.FOLDERID_CommonStartMenu, true},
		{"任务栏和快速启动", windows.FOLDERID_QuickLaunch, true},
		{"发送到", windows.FOLDERID_SendTo, false},
	}
	var out []shortcutPlace
	for _, w := range want {
		dir, err := windows.KnownFolderPath(w.id, windows.KF_FLAG_DEFAULT)
		if err != nil || dir == "" {
			continue
		}
		out = append(out, shortcutPlace{name: w.name, dir: dir, deep: w.deep})
	}
	return out
}

// decodeANSI 按系统代码页把字节串转成字符串(中文系统上是 GBK)
func decodeANSI(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const cpACP = 0
	n, err := windows.MultiByteToWideChar(cpACP, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		return string(b)
	}
	u := make([]uint16, n)
	if _, err := windows.MultiByteToWideChar(cpACP, 0, &b[0], int32(len(b)), &u[0], n); err != nil {
		return string(b)
	}
	return windows.UTF16ToString(u)
}
