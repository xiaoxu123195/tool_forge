//go:build darwin

package diskclean

import (
	"io/fs"
	"syscall"
)

// sfDataless iCloud「优化存储」把文件内容挪到云端后打的标记
const sfDataless = 0x40000000

func isDataless(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Flags&sfDataless != 0
}
