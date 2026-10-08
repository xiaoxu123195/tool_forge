package diskclean

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Verdict 守卫对一个路径的判断
type Verdict struct {
	Blocked bool
	// Reason Blocked 时:为什么不能删。前端原样显示,所以要写成人话,能指路的指个路
	Reason string
	// Warn 能删,但删之前该知道的事(虚拟机磁盘之类)
	Warn string
}

func blocked(reason string) Verdict { return Verdict{Blocked: true, Reason: reason} }

// treeSpec 一棵整体受保护的目录树
type treeSpec struct {
	root   string
	reason string
	// open 树里这些子树的"内容"可以删,子树的根本身仍然受保护。
	// Windows\Temp 这个目录删不得,但它里面的东西本来就是拿来删的
	open []string
}

// guardSpec 一个平台的保护规则。和平台无关的判断逻辑在 Guard 上,
// 这里只是数据 —— 测试拿假的系统路径构造它,不用真去碰 C:\Windows
type guardSpec struct {
	trees []treeSpec
	// files 具体某个文件不能删(路径 → 原因)
	files map[string]string
	// allowFiles 受保护树里单独放行的文件,比如 Windows\MEMORY.DMP
	allowFiles []string
	// rootNames 任何一块盘的根目录下,这些名字(文件或目录)都不能删
	rootNames map[string]string
	// baseNames 不管在哪儿,叫这个名字的文件都不能删(注册表文件)
	baseNames map[string]string
	// noWipe 这些目录可以删里面的个别文件,但不能被整个清空(家目录、文档……)
	noWipe []string
	// noWipeParents 这些目录的直接子目录都不能被整个清空(C:\Users 下面每个人的家)
	noWipeParents []string
	// appDataName 个人目录下放程序数据的那个目录叫什么:Windows 上是 AppData,macOS 上是 Library
	appDataName string
}

// Guard 判断一个路径能不能删。
//
// 所有删除最后都过它,不管请求是从哪个页面、哪条规则来的。
// 判断只看规范化之后的路径字符串;CheckFinal 另外把链接解到底再判一次
type Guard struct {
	trees         []treeSpec // 按根的长度从长到短:最具体的那棵先匹配
	files         map[string]string
	allowFiles    map[string]bool
	rootNames     map[string]string
	baseNames     map[string]string
	noWipe        map[string]bool
	noWipeParents map[string]bool
	appDataName   string
}

func newGuard(spec guardSpec) *Guard {
	g := &Guard{
		files:         map[string]string{},
		allowFiles:    map[string]bool{},
		rootNames:     map[string]string{},
		baseNames:     map[string]string{},
		noWipe:        map[string]bool{},
		noWipeParents: map[string]bool{},
		appDataName:   strings.ToLower(spec.appDataName),
	}
	for _, t := range spec.trees {
		if t.root == "" {
			continue // 环境变量没取到值。宁可少一条规则,也不能让空串变成 "." 去匹配别的
		}
		open := make([]string, 0, len(t.open))
		for _, o := range t.open {
			open = append(open, norm(o))
		}
		g.trees = append(g.trees, treeSpec{root: norm(t.root), reason: t.reason, open: open})
	}
	sort.SliceStable(g.trees, func(i, j int) bool { return len(g.trees[i].root) > len(g.trees[j].root) })
	for p, r := range spec.files {
		g.files[norm(p)] = r
	}
	for _, p := range spec.allowFiles {
		g.allowFiles[norm(p)] = true
	}
	for n, r := range spec.rootNames {
		g.rootNames[strings.ToLower(n)] = r
	}
	for n, r := range spec.baseNames {
		g.baseNames[strings.ToLower(n)] = r
	}
	for _, p := range spec.noWipe {
		if p != "" {
			g.noWipe[norm(p)] = true
		}
	}
	for _, p := range spec.noWipeParents {
		if p != "" {
			g.noWipeParents[norm(p)] = true
		}
	}
	return g
}

// Check 能不能删 path 这个条目本身
func (g *Guard) Check(path string) Verdict {
	if path == "" || !filepath.IsAbs(path) {
		return blocked("路径不完整")
	}
	p := norm(path)
	if isVolumeRoot(p) {
		return blocked("这是整块盘")
	}
	if r, ok := g.files[p]; ok {
		return blocked(r)
	}
	if r, ok := g.rootNames[firstComponent(p)]; ok {
		return blocked(r)
	}
	if r, ok := g.baseNames[filepath.Base(p)]; ok {
		return blocked(r)
	}
	if g.allowFiles[p] {
		return Verdict{}
	}
	for _, t := range g.trees {
		if !within(p, t.root) {
			continue
		}
		// 最具体的那棵树说了算:落在它放行的子树里就放行,否则就拦
		for _, o := range t.open {
			if under(p, o) {
				return Verdict{Warn: warnFor(p)}
			}
		}
		return blocked(t.reason)
	}
	return Verdict{Warn: warnFor(p)}
}

// programDir 放在文档、个人目录里,但归程序管的目录
type programDir struct {
	owner string
	// sync 网盘的同步目录:在这里删,云端那份也跟着删
	sync bool
}

// programDirs 按目录名认。两种后果不一样,提醒也分开说:
//   - 聊天软件的数据目录:文件被聊天记录引用着,删了点开就是"文件已过期或已被清理"
//   - 网盘的同步目录:删了会同步到云端,网盘里的那份也没了
var programDirs = map[string]programDir{
	"wechat files":    {"微信", false},
	"xwechat_files":   {"微信", false},
	"tencent files":   {"QQ", false},
	"wxwork":          {"企业微信", false},
	"wps cloud files": {"WPS 云文档", true},
	"wpsdrive":        {"WPS 云盘", true},
	"onedrive":        {"OneDrive", true},
	"dropbox":         {"Dropbox", true},
	"iclouddrive":     {"iCloud", true},
	"google drive":    {"Google 云端硬盘", true},
}

// programDirOf 一个目录名是不是程序的数据目录。OneDrive 的企业版叫「OneDrive - 公司名」
func programDirOf(name string) (programDir, bool) {
	name = strings.ToLower(name)
	if d, ok := programDirs[name]; ok {
		return d, true
	}
	if strings.HasPrefix(name, "onedrive - ") {
		return programDirs["onedrive"], true
	}
	return programDir{}, false
}

// programOwner p 在不在某个程序的数据目录里
func programOwner(p string) (programDir, bool) {
	for _, part := range strings.Split(filepath.Clean(p), string(filepath.Separator)) {
		if d, ok := programDirOf(part); ok {
			return d, true
		}
	}
	return programDir{}, false
}

// CheckContents 能不能清掉 dir 里面的东西(dir 本身留着)。缓存规则走这条
func (g *Guard) CheckContents(dir string) Verdict {
	if dir == "" || !filepath.IsAbs(dir) {
		return blocked("路径不完整")
	}
	p := norm(dir)
	if isVolumeRoot(p) || g.noWipe[p] || g.noWipeParents[norm(filepath.Dir(p))] {
		return blocked("这个目录装着大量用户数据,不能整个清空")
	}
	// 太浅的目录不可能是哪个程序的缓存目录。一条写错的规则(比如环境变量没取到值)
	// 会退化成 C:\ 或 C:\Users 这种地方,这条兜住
	if depth(p) < 2 {
		return blocked("目录层级太浅,不像是缓存目录")
	}
	return g.Check(filepath.Join(p, "x"))
}

// Structural 个人目录本身,以及个人目录下面直接那一层(桌面、文档、图片、联系人……)。
// 这些是系统和程序认定该在的位置,空着也不该删 —— 删了有的程序会出错,有的系统会再建回来
func (g *Guard) Structural(dir string) bool {
	p := norm(dir)
	if g.noWipe[p] {
		return true
	}
	parent := norm(filepath.Dir(p))
	return g.noWipeParents[parent] || g.noWipeParents[norm(filepath.Dir(parent))]
}

// IsAppData 个人目录下放程序数据的那个目录(Windows 的 AppData、macOS 的 Library)。
// 里面的空目录是程序自己建的,删了腾不出空间,个别程序找不到还会出错
func (g *Guard) IsAppData(dir string) bool {
	if g.appDataName == "" || strings.ToLower(filepath.Base(dir)) != g.appDataName {
		return false
	}
	return g.noWipeParents[norm(filepath.Dir(filepath.Dir(dir)))]
}

// CheckFinal 和 Check 一样,另外把路径解到底(链接、目录联接、短文件名)再判一次,
// 两次都放行才放行。光看字符串的话,一个指向系统目录的联接就能把所有规则绕过去
func (g *Guard) CheckFinal(path string) Verdict {
	v := g.Check(path)
	if v.Blocked {
		return v
	}
	real, err := finalPath(path)
	if err != nil || norm(real) == norm(path) {
		return v
	}
	if rv := g.Check(real); rv.Blocked {
		return blocked(fmt.Sprintf("它实际在 %s:%s", real, rv.Reason))
	}
	return v
}

// CheckContentsFinal CheckContents 的"解到底"版本,缓存目录用
func (g *Guard) CheckContentsFinal(dir string) Verdict {
	v := g.CheckContents(dir)
	if v.Blocked {
		return v
	}
	real, err := finalPath(dir)
	if err != nil || norm(real) == norm(dir) {
		return v
	}
	if rv := g.CheckContents(real); rv.Blocked {
		return blocked(fmt.Sprintf("这个目录实际指向 %s:%s", real, rv.Reason))
	}
	return v
}

// warnExt 能删,但删之前要知道的文件类型
var warnExt = map[string]string{
	".vhdx":  "虚拟机 / WSL / Docker 的磁盘镜像:删掉等于把里面的系统和数据一起删了",
	".vhd":   "虚拟机的磁盘镜像:删掉等于把里面的系统和数据一起删了",
	".avhdx": "Hyper-V 检查点的差异磁盘:删了对应的虚拟机会坏掉",
	".vmdk":  "VMware 虚拟机的磁盘:删掉等于把里面的系统和数据一起删了",
	".vdi":   "VirtualBox 虚拟机的磁盘:删掉等于把里面的系统和数据一起删了",
	".qcow2": "虚拟机的磁盘镜像:删掉等于把里面的系统和数据一起删了",
	".pst":   "Outlook 本地邮件数据,可能是唯一的一份",
	".ost":   "Outlook 离线邮箱缓存:账号还在的话能重新同步,账号没了就是唯一的一份",
	".kdbx":  "KeePass 密码库,删了里面的密码就没了",
}

func warnFor(p string) string {
	if w := warnExt[strings.ToLower(filepath.Ext(p))]; w != "" {
		return w
	}
	if d, ok := programOwner(p); ok {
		if d.sync {
			return "这是" + d.owner + "的同步目录:在这里删,云端的那份也会跟着删掉"
		}
		return "这是" + d.owner + "自己的数据目录:删了以后,聊天记录里用到它的图片和文件会打不开"
	}
	return ""
}

// appDirs 工具箱自己的数据目录和程序所在目录:删了工具箱自己就坏了
func appDirs() []treeSpec {
	var out []treeSpec
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, treeSpec{root: filepath.Join(home, ".toolforge"), reason: "工具箱自己的数据"})
	}
	if exe, err := os.Executable(); err == nil {
		out = append(out, treeSpec{root: filepath.Dir(exe), reason: "工具箱程序所在的目录"})
	}
	return out
}

// ---------------- 路径工具 ----------------

// norm 规范化成可以直接比较的形式:Clean 过、小写。
// 小写是因为 Windows 和 macOS 默认都不区分大小写 —— 宁可多拦,不能少拦
func norm(p string) string { return strings.ToLower(filepath.Clean(p)) }

// within p 就是 root,或者在 root 里面
func within(p, root string) bool { return p == root || under(p, root) }

// under p 在 root 里面,不含 root 本身。两个参数都得是 norm 过的
func under(p, root string) bool {
	if len(p) <= len(root) || !strings.HasPrefix(p, root) {
		return false
	}
	// root 是 C:\ 或 / 这种自带分隔符结尾的
	if strings.HasSuffix(root, string(filepath.Separator)) {
		return true
	}
	return p[len(root)] == filepath.Separator
}

// isVolumeRoot C:\、/ 这种整块盘的根
func isVolumeRoot(p string) bool {
	vol := filepath.VolumeName(p)
	rest := strings.Trim(p[len(vol):], string(filepath.Separator))
	return rest == ""
}

// firstComponent 盘根下的第一段:C:\pagefile.sys → pagefile.sys
func firstComponent(p string) string {
	rest := strings.TrimLeft(p[len(filepath.VolumeName(p)):], string(filepath.Separator))
	if i := strings.IndexByte(rest, filepath.Separator); i >= 0 {
		return rest[:i]
	}
	return rest
}

// depth 盘根下有几层:C:\a\b → 2
func depth(p string) int {
	rest := strings.Trim(p[len(filepath.VolumeName(p)):], string(filepath.Separator))
	if rest == "" {
		return 0
	}
	return strings.Count(rest, string(filepath.Separator)) + 1
}
