//go:build windows

package diskclean

import (
	"os"
	"path/filepath"
)

// winEnv 构造保护规则要用到的几个系统位置。
// 单独抽出来是为了测试:拿一套假路径构造 Guard,不用真去碰 C:\Windows
type winEnv struct {
	systemRoot   string // C:\Windows
	systemDrive  string // C:
	programFiles []string
	programData  string
	home         string
	localAppData string
	appData      string
}

func envFromOS() winEnv {
	e := winEnv{
		systemRoot:   os.Getenv("SystemRoot"),
		systemDrive:  os.Getenv("SystemDrive"),
		programData:  os.Getenv("ProgramData"),
		localAppData: os.Getenv("LOCALAPPDATA"),
		appData:      os.Getenv("APPDATA"),
	}
	if e.systemRoot == "" {
		e.systemRoot = `C:\Windows`
	}
	if e.systemDrive == "" {
		e.systemDrive = filepath.VolumeName(e.systemRoot)
	}
	for _, k := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
		if v := os.Getenv(k); v != "" {
			e.programFiles = append(e.programFiles, v)
		}
	}
	e.home, _ = os.UserHomeDir()
	return e
}

// NewGuard 按本机的系统位置构造守卫
func NewGuard() *Guard {
	spec := windowsSpec(envFromOS())
	spec.trees = append(spec.trees, appDirs()...)
	return newGuard(spec)
}

func windowsSpec(e winEnv) guardSpec {
	sr, sd := e.systemRoot, e.systemDrive
	pd := e.programData
	wer := pd + `\Microsoft\Windows\WER`

	s := guardSpec{
		trees: []treeSpec{
			{sr, "Windows 系统目录", []string{
				sr + `\Temp`,
				sr + `\SoftwareDistribution\Download`,
				sr + `\Minidump`,
				sr + `\Logs\CBS`,
			}},
			{sr + `\WinSxS`, "系统组件存储:看着大是因为里面大量是硬链接。要瘦身用系统自带的「磁盘清理 → 清理系统文件」", nil},
			{sr + `\Installer`, "Windows 安装缓存:删了以后已经装好的软件没法修复、更新和卸载", nil},
			{sr + `\System32`, "Windows 核心文件", nil},
			{sd + `\Windows.old`, "旧版 Windows 的备份:用系统自带的「磁盘清理 → 清理系统文件 → 以前的 Windows 安装」来删", nil},
			{sd + `\Recovery`, "系统恢复环境", nil},
			{sd + `\Boot`, "启动文件", nil},
			{sd + `\EFI`, "启动文件", nil},
			{sd + `\$WinREAgent`, "系统更新的临时目录", nil},
			{sd + `\$Windows.~BT`, "系统升级的临时目录:用系统自带的「磁盘清理」来删", nil},
			{sd + `\$Windows.~WS`, "系统升级的临时目录:用系统自带的「磁盘清理」来删", nil},
			{sd + `\$SysReset`, "系统重置的临时目录", nil},
			{sd + `\Config.Msi`, "软件安装回滚用的文件", nil},
			{sd + `\Users\Default`, "新建用户时套用的模板", nil},
		},
		files: map[string]string{
			sd + `\bootmgr`: "启动文件",
			sd + `\BOOTNXT`: "启动文件",
		},
		allowFiles: []string{sr + `\MEMORY.DMP`},
		// 每块 NTFS 盘的根下都可能有这些
		rootNames: map[string]string{
			"pagefile.sys":              "虚拟内存文件:由系统管理。要调大小去「系统属性 → 高级 → 性能设置 → 虚拟内存」",
			"hiberfil.sys":              "休眠文件:不用休眠的话,以管理员身份运行 powercfg -h off,系统会自己把它删掉",
			"swapfile.sys":              "系统交换文件:由系统管理",
			"dumpstack.log":             "系统启动日志:由系统管理",
			"dumpstack.log.tmp":         "系统启动日志:由系统管理",
			"system volume information": "系统还原点和卷影副本:要清理去「系统保护」里删还原点",
			"$recycle.bin":              "回收站:请用「清空回收站」",
		},
		baseNames: map[string]string{
			"ntuser.dat":        "用户的注册表文件:删了这个用户就登不进去了",
			"ntuser.dat.log1":   "用户的注册表文件:删了这个用户就登不进去了",
			"ntuser.dat.log2":   "用户的注册表文件:删了这个用户就登不进去了",
			"ntuser.ini":        "用户的注册表文件:删了这个用户就登不进去了",
			"usrclass.dat":      "用户的注册表文件:删了这个用户就登不进去了",
			"usrclass.dat.log1": "用户的注册表文件:删了这个用户就登不进去了",
			"usrclass.dat.log2": "用户的注册表文件:删了这个用户就登不进去了",
		},
		noWipeParents: []string{sd + `\Users`},
	}

	for _, pf := range e.programFiles {
		s.trees = append(s.trees, treeSpec{pf, "程序安装目录:要删软件请走卸载,直接删会留下一堆坏掉的注册信息", nil})
	}
	if pd != "" {
		s.trees = append(s.trees, treeSpec{pd, "所有用户共享的程序数据:删了可能导致软件坏掉", []string{
			wer + `\ReportArchive`,
			wer + `\ReportQueue`,
			wer + `\Temp`,
		}})
	}
	if h := e.home; h != "" {
		s.trees = append(s.trees,
			treeSpec{h + `\.ssh`, "SSH 密钥", nil},
			treeSpec{h + `\.gnupg`, "GPG 密钥", nil},
		)
		s.noWipe = append(s.noWipe,
			h, h+`\Desktop`, h+`\Documents`, h+`\Downloads`, h+`\Pictures`, h+`\Videos`, h+`\Music`,
			h+`\OneDrive`, h+`\AppData`, h+`\AppData\LocalLow`,
		)
	}
	s.noWipe = append(s.noWipe, sd+`\Users`, e.localAppData, e.appData)
	return s
}
