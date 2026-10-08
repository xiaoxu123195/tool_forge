//go:build !windows

package diskclean

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// isLinkLike 没下载到本地的网盘文件:读一下就会触发下载
func isLinkLike(fi fs.FileInfo) bool { return isDataless(fi) }

func finalPath(p string) (string, error) { return filepath.EvalSymlinks(p) }

// fileID 同一个文件的不同名字(硬链接)拿到同一个 ID
func fileID(p string) (string, bool) {
	fi, err := os.Lstat(p)
	if err != nil {
		return "", false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), true
}

func isInUse(error) bool { return false }

// volumes 只给个人目录所在的那块盘。从 / 往下扫会经过系统卷和各种挂载点,
// 同一个文件能从两条路径走到,扫出来的结果里会有重影
func volumes() []Volume {
	home, err := os.UserHomeDir()
	if err != nil {
		return []Volume{}
	}
	var st syscall.Statfs_t
	if syscall.Statfs(home, &st) != nil {
		return []Volume{}
	}
	bs := uint64(st.Bsize)
	return []Volume{{
		Path:   home,
		Label:  "个人目录",
		Total:  int64(st.Blocks * bs),
		Free:   int64(st.Bavail * bs),
		System: true,
	}}
}

func isElevated() bool { return os.Geteuid() == 0 }

func runningProcs() map[string]bool { return map[string]bool{} }

func recycleBinInfo() (int64, int64, error) { return 0, 0, errors.New("这个系统上不支持") }

func emptyRecycleBin() error { return errors.New("这个系统上不支持") }

// moveToTrash 挪进废纸篓。只认和废纸篓在同一块盘上的文件 ——
// 跨盘的话 rename 做不到,而先拷再删对大文件来说既慢又可能半途失败
func moveToTrash(p string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	trash := filepath.Join(home, ".Trash")
	name := filepath.Base(p)
	dst := filepath.Join(trash, name)
	if _, err := os.Lstat(dst); err == nil {
		ext := filepath.Ext(name)
		dst = filepath.Join(trash, fmt.Sprintf("%s %s%s",
			strings.TrimSuffix(name, ext), time.Now().Format("15.04.05"), ext))
	}
	if err := os.Rename(p, dst); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return errors.New("这个文件在别的盘上,没法挪进废纸篓;可以选永久删除")
		}
		return err
	}
	return nil
}
