package diskclean

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

// .lnk(Shell Link)文件的解析,只取判断"它指向的东西还在不在"要用的部分。
//
// 格式是公开的(MS-SHLLINK)。不走 IShellLink 的那套解析:它的 Resolve 会为了"修复"
// 失效的链接主动去搜索、甚至改写快捷方式文件 —— 我们只想看,不想让它动。
//
// 目标路径可能记在三个地方,可靠程度不一样:
//   - 项目标识列表(IDList):系统自己最先用的就是它,几乎每个快捷方式都有
//   - LinkInfo:本地路径加上盘的类型,有的快捷方式没有
//   - 相对路径:按快捷方式最初建的位置算的,快捷方式被挪过之后就对不上了
//     (安装程序在别处建好再拷进开始菜单,就是这样)

const (
	lnkHasIDList     = 1 << 0
	lnkHasLinkInfo   = 1 << 1
	lnkHasName       = 1 << 2
	lnkHasRelPath    = 1 << 3
	lnkHasWorkingDir = 1 << 4
	lnkHasArguments  = 1 << 5
	lnkHasIconLoc    = 1 << 6
	lnkIsUnicode     = 1 << 7
	lnkHasDarwinID   = 1 << 12

	sigEnvironmentBlock = 0xA0000001
	sigDarwinBlock      = 0xA0000006

	linkInfoLocal   = 1 << 0 // VolumeIDAndLocalBasePath
	linkInfoNetwork = 1 << 1 // CommonNetworkRelativeLinkAndPathSuffix
)

// lnkHeaderCLSID 文件头里固定的那个 CLSID
var lnkHeaderCLSID = []byte{0x01, 0x14, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}

// lnkInfo 从快捷方式里读出来的目标信息
type lnkInfo struct {
	// idList 项目标识列表的原始字节(含结尾的两个 0),交给系统转成路径
	idList []byte
	// localPath 本地路径(LinkInfo 里记的)
	localPath string
	// envPath 环境变量块里的路径,原样带着 %VAR%
	envPath string
	// relPath 相对 .lnk 所在目录的路径
	relPath string
	// driveType 目标所在的盘是什么类型(DRIVE_FIXED 之类);0 表示没记
	driveType uint32
	// network 目标在网络位置上
	network bool
	// darwin Windows Installer 的"广告"快捷方式:指向的是安装器里的描述符,不是文件
	darwin bool
}

var errNotLnk = errors.New("不是快捷方式文件")

// parseLnk 解析 .lnk。decodeANSI 把系统代码页的字节串转成字符串 ——
// 老程序建的快捷方式里路径是按系统代码页(中文系统上是 GBK)存的,不是 UTF-8
func parseLnk(data []byte, decodeANSI func([]byte) string) (*lnkInfo, error) {
	r := &lnkReader{b: data}
	if r.u32(0) != 0x4C || len(data) < 0x4C || string(data[4:20]) != string(lnkHeaderCLSID) {
		return nil, errNotLnk
	}
	flags := r.u32(20)
	info := &lnkInfo{darwin: flags&lnkHasDarwinID != 0}
	pos := 0x4C

	if flags&lnkHasIDList != 0 {
		size := int(r.u16(pos))
		if end := pos + 2 + size; end <= len(data) && validIDList(data[pos+2:end]) {
			info.idList = data[pos+2 : end]
		}
		pos += 2 + size
	}
	if flags&lnkHasLinkInfo != 0 {
		size := int(r.u32(pos))
		if size < 0x1C || pos+size > len(data) {
			return nil, errors.New("快捷方式里的 LinkInfo 长度不对")
		}
		li := &lnkReader{b: data[pos : pos+size]}
		headerSize := li.u32(4)
		liFlags := li.u32(8)
		if liFlags&linkInfoLocal != 0 {
			if vol := int(li.u32(12)); vol > 0 && vol+8 <= size {
				info.driveType = li.u32(vol + 4)
			}
			base, suffix := "", ""
			// 头够长时有 Unicode 版本的路径,优先用它;没有才退回系统代码页的那份
			if headerSize >= 0x24 {
				base = li.utf16z(int(li.u32(28)))
				suffix = li.utf16z(int(li.u32(32)))
			}
			if base == "" {
				base = decodeANSI(li.ansiz(int(li.u32(16))))
				suffix = decodeANSI(li.ansiz(int(li.u32(24))))
			}
			info.localPath = joinSuffix(base, suffix)
		}
		info.network = liFlags&linkInfoNetwork != 0 && info.localPath == ""
		pos += size
	}

	// 字符串区:名称、相对路径、工作目录、参数、图标,按顺序、有哪个读哪个
	unicode := flags&lnkIsUnicode != 0
	for _, f := range []uint32{lnkHasName, lnkHasRelPath, lnkHasWorkingDir, lnkHasArguments, lnkHasIconLoc} {
		if flags&f == 0 {
			continue
		}
		n := int(r.u16(pos))
		pos += 2
		width := 1
		if unicode {
			width = 2
		}
		end := pos + n*width
		if end > len(data) {
			return info, nil // 截断了:前面读到的照样能用
		}
		if f == lnkHasRelPath {
			if unicode {
				info.relPath = decodeUTF16(data[pos:end])
			} else {
				info.relPath = decodeANSI(data[pos:end])
			}
		}
		pos = end
	}

	// 附加数据块:环境变量块、安装器块
	for pos+8 <= len(data) {
		size := int(r.u32(pos))
		if size < 8 || pos+size > len(data) {
			break
		}
		switch r.u32(pos + 4) {
		case sigEnvironmentBlock:
			// 260 字节的 ANSI 版本后面跟着 520 字节的 Unicode 版本
			blk := &lnkReader{b: data[pos : pos+size]}
			info.envPath = blk.utf16z(8 + 260)
			if info.envPath == "" {
				info.envPath = decodeANSI(blk.ansiz(8))
			}
		case sigDarwinBlock:
			info.darwin = true
		}
		pos += size
	}
	return info, nil
}

// validIDList 结构对不对:一串"长度 + 内容"的项,以长度为 0 的项结尾,而且正好结束在末尾。
// 不对的不交给系统去转 —— 系统的解析代码在我们自己的进程里跑,坏数据可能让它出事
func validIDList(b []byte) bool {
	pos := 0
	for pos+2 <= len(b) {
		n := int(binary.LittleEndian.Uint16(b[pos:]))
		if n == 0 {
			return pos+2 == len(b) && pos > 0
		}
		if n < 3 || pos+n > len(b) {
			return false
		}
		pos += n
	}
	return false
}

func joinSuffix(base, suffix string) string {
	if suffix == "" || base == "" {
		return base
	}
	if base[len(base)-1] == '\\' {
		return base + suffix
	}
	return base + `\` + suffix
}

// lnkReader 越界一律读成零值而不是 panic:快捷方式文件可能是坏的、截断的、故意构造的
type lnkReader struct{ b []byte }

func (r *lnkReader) u16(off int) uint16 {
	if off < 0 || off+2 > len(r.b) {
		return 0
	}
	return binary.LittleEndian.Uint16(r.b[off:])
}

func (r *lnkReader) u32(off int) uint32 {
	if off < 0 || off+4 > len(r.b) {
		return 0
	}
	return binary.LittleEndian.Uint32(r.b[off:])
}

// ansiz 从 off 开始到第一个 0 字节
func (r *lnkReader) ansiz(off int) []byte {
	if off <= 0 || off >= len(r.b) {
		return nil
	}
	end := off
	for end < len(r.b) && r.b[end] != 0 {
		end++
	}
	return r.b[off:end]
}

// utf16z 从 off 开始到第一个 0 字符
func (r *lnkReader) utf16z(off int) string {
	if off <= 0 || off >= len(r.b) {
		return ""
	}
	end := off
	for end+1 < len(r.b) && (r.b[end] != 0 || r.b[end+1] != 0) {
		end += 2
	}
	return decodeUTF16(r.b[off:end])
}

func decodeUTF16(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, binary.LittleEndian.Uint16(b[i:]))
	}
	for len(u) > 0 && u[len(u)-1] == 0 {
		u = u[:len(u)-1]
	}
	return string(utf16.Decode(u))
}
