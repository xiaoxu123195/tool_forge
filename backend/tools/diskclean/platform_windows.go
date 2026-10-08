//go:build windows

package diskclean

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// isLinkLike 重解析点(符号链接、目录联接、OneDrive 之类的网盘文件)和离线文件。
//
// 直接看属性位,不靠 Go 翻译出来的 Mode:挂载点在不同 Go 版本、
// 不同 GODEBUG 设置下报的 Mode 不一样,而这里错一次就是跟进了一个联接
func isLinkLike(fi fs.FileInfo) bool {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	const mask = windows.FILE_ATTRIBUTE_REPARSE_POINT | windows.FILE_ATTRIBUTE_OFFLINE |
		windows.FILE_ATTRIBUTE_RECALL_ON_OPEN | windows.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS
	return d.FileAttributes&mask != 0
}

func openForQuery(p string, flags uint32) (windows.Handle, error) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return 0, err
	}
	// 只要读属性的权限:不读内容,也就不会触发网盘文件的下载
	return windows.CreateFile(u, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|flags, 0)
}

// finalPath 把路径里的链接、目录联接、短文件名都解到底,得到它在磁盘上真正的位置
func finalPath(p string) (string, error) {
	h, err := openForQuery(p, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", err
		}
		if int(n) < len(buf) {
			s := windows.UTF16ToString(buf[:n])
			switch {
			case strings.HasPrefix(s, `\\?\UNC\`):
				return `\\` + s[len(`\\?\UNC\`):], nil
			case strings.HasPrefix(s, `\\?\`):
				return s[len(`\\?\`):], nil
			}
			return s, nil
		}
		buf = make([]uint16, n+1)
	}
}

// fileID 同一个文件的不同名字(硬链接)拿到同一个 ID
func fileID(p string) (string, bool) {
	h, err := openForQuery(p, windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return "", false
	}
	return fmt.Sprintf("%x:%x:%x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), true
}

func isInUse(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// volumes 本机的固定盘和可移动盘。网络盘不列:扫得慢,删的风险也是另一回事
func volumes() []Volume {
	out := []Volume{}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return out
	}
	sysDrive := strings.ToUpper(os.Getenv("SystemDrive"))
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		u, _ := windows.UTF16PtrFromString(root)
		t := windows.GetDriveType(u)
		if t != windows.DRIVE_FIXED && t != windows.DRIVE_REMOVABLE {
			continue
		}
		var free, total, totalFree uint64
		if windows.GetDiskFreeSpaceEx(u, &free, &total, &totalFree) != nil {
			continue // 读卡器里没插卡之类
		}
		v := Volume{
			Path:      root,
			Total:     int64(total),
			Free:      int64(free),
			System:    root[:2] == sysDrive,
			Removable: t == windows.DRIVE_REMOVABLE,
		}
		label := make([]uint16, windows.MAX_PATH+1)
		if windows.GetVolumeInformation(u, &label[0], uint32(len(label)), nil, nil, nil, nil, 0) == nil {
			v.Label = windows.UTF16ToString(label)
		}
		out = append(out, v)
	}
	return out
}

func isElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// runningProcs 正在运行的进程名(小写)
func runningProcs() map[string]bool {
	out := map[string]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))] = true
	}
	return out
}

var (
	shell32                = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOperationW   = shell32.NewProc("SHFileOperationW")
	procSHQueryRecycleBinW = shell32.NewProc("SHQueryRecycleBinW")
	procSHEmptyRecycleBinW = shell32.NewProc("SHEmptyRecycleBinW")
)

// shQueryRBInfo 对应 SHQUERYRBINFO(64 位下按自然对齐,cbSize 后面有 4 字节空洞)
type shQueryRBInfo struct {
	cbSize      uint32
	i64Size     int64
	i64NumItems int64
}

// shellCall 回收站这几个外壳函数内部要用 COM,得在单线程套间里调:
// 把 goroutine 钉在当前线程上,初始化好再调,调完收拾干净
func shellCall(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_OK 和 S_FALSE(这个线程已经初始化过)都要配一次 CoUninitialize
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil || err == syscall.Errno(1) {
		defer windows.CoUninitialize()
	}
	fn()
}

// recycleBinInfo 所有盘的回收站加起来多大、几个东西
func recycleBinInfo() (size, items int64, err error) {
	info := shQueryRBInfo{}
	info.cbSize = uint32(unsafe.Sizeof(info))
	var r uintptr
	shellCall(func() { r, _, _ = procSHQueryRecycleBinW.Call(0, uintptr(unsafe.Pointer(&info))) })
	if uint32(r) != 0 {
		return 0, 0, fmt.Errorf("查询回收站失败(%#x)", uint32(r))
	}
	return info.i64Size, info.i64NumItems, nil
}

func emptyRecycleBin() error {
	const flags = 0x1 | 0x2 | 0x4 // 不确认、不显示进度、不放提示音
	var r uintptr
	shellCall(func() { r, _, _ = procSHEmptyRecycleBinW.Call(0, 0, flags) })
	// 回收站本来就空着时它返回 E_UNEXPECTED,不算失败
	if hr := uint32(r); hr != 0 && hr != 0x8000FFFF {
		return fmt.Errorf("清空回收站失败(%#x)", hr)
	}
	return nil
}

// cleanDeliveryOptimization 用系统自带的命令清传递优化缓存。
//
// 那个目录归传递优化服务管,服务开着时直接删文件是在和它抢;
// 它自己的命令会先让服务放手,清完账也是对的
const deliveryOptimizationScript = `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
try {
  Delete-DeliveryOptimizationCache -Force -ErrorAction Stop
} catch {
  Write-Output $_.Exception.Message
  exit 1
}`

func cleanDeliveryOptimization(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeCommand(deliveryOptimizationScript))
	// Wails 是 GUI 子系统,不藏的话会闪一个黑框
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = strings.TrimSpace(msg[:i])
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("系统的清理命令没成功:%s", msg)
	}
	return nil
}

// encodeCommand PowerShell 的 -EncodedCommand 要 UTF-16LE 的 base64
func encodeCommand(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// shFileOpStruct 对应 SHFILEOPSTRUCTW(64 位自然对齐)
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete           = 0x0003
	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000
)

// moveToTrash 移到回收站。
//
// FOF_WANTNUKEWARNING 是关键的一位:文件大到放不进回收站(或者用户把回收站关了)时,
// 只带"不要确认"的话系统会一声不吭地直接永久删除 —— 而用户选的明明是"进回收站"。
// 带上它,系统会在那一刻弹窗再问一次
func moveToTrash(p string) error {
	// pFrom 是以两个 NUL 结尾的路径列表,只有一个的话它会接着往后读
	from, err := windows.UTF16FromString(p)
	if err != nil {
		return err
	}
	from = append(from, 0)
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofNoErrorUI | fofSilent | fofWantNukeWarning,
	}
	var r uintptr
	shellCall(func() { r, _, _ = procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op))) })
	runtime.KeepAlive(from)
	if op.fAnyOperationsAborted != 0 {
		return errTrashAborted
	}
	if code := uint32(r); code != 0 {
		return fmt.Errorf("移到回收站失败(代码 %#x)", code)
	}
	// 文件被占用时它有时返回 0 却什么都没做。删没删,以磁盘为准
	if _, err := os.Lstat(p); err == nil {
		return errors.New("没能移到回收站,文件可能正被别的程序占用")
	}
	return nil
}
