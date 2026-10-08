//go:build windows

package diskclean

import (
	"os"
	"strings"
	"testing"
)

func fakeEnv() winEnv {
	return winEnv{
		systemRoot:   `C:\Windows`,
		systemDrive:  `C:`,
		programFiles: []string{`C:\Program Files`, `C:\Program Files (x86)`},
		programData:  `C:\ProgramData`,
		home:         `C:\Users\u`,
		localAppData: `C:\Users\u\AppData\Local`,
		appData:      `C:\Users\u\AppData\Roaming`,
	}
}

func TestGuardCheck(t *testing.T) {
	g := newGuard(windowsSpec(fakeEnv()))
	cases := []struct {
		path    string
		blocked bool
		reason  string // 原因里要出现的字,能指路的要指对路
		warn    string
	}{
		{`C:\`, true, "整块盘", ""},
		{`D:\`, true, "整块盘", ""},
		{`C:\Windows\System32\kernel32.dll`, true, "核心", ""},
		{`c:\WINDOWS\system32\KERNEL32.DLL`, true, "核心", ""},
		{`C:\Users\u\..\..\Windows\notepad.exe`, true, "系统目录", ""},
		{`C:\Windows\WinSxS\amd64_x\a.dll`, true, "磁盘清理", ""},
		{`C:\Windows\Installer\abc.msi`, true, "卸载", ""},
		// Temp 这个目录本身删不得,里面的东西可以
		{`C:\Windows\Temp`, true, "系统目录", ""},
		{`C:\Windows\Temp\setup.log`, false, "", ""},
		{`C:\Windows\MEMORY.DMP`, false, "", ""},
		// 放行的只有 Download,同一个父目录下的别的东西照样拦
		{`C:\Windows\SoftwareDistribution\DataStore\DataStore.edb`, true, "系统目录", ""},
		{`C:\Windows\SoftwareDistribution\Download\abc\update.cab`, false, "", ""},
		{`C:\Program Files\App\app.exe`, true, "卸载", ""},
		{`C:\Program Files (x86)\App\app.exe`, true, "卸载", ""},
		{`C:\ProgramData\Vendor\db.dat`, true, "共享", ""},
		{`C:\ProgramData\Microsoft\Windows\WER\ReportQueue\r1\Report.wer`, false, "", ""},
		// 所有用户的开始菜单里全是快捷方式,清无效快捷方式要能动它
		{`C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Old App.lnk`, false, "", ""},
		{`C:\ProgramData\Microsoft\Windows\Start Menu`, true, "共享", ""},
		// 传递优化缓存不能直接删,但要告诉人该去哪儿清
		{`C:\Windows\ServiceProfiles\NetworkService\AppData\Local\Microsoft\Windows\DeliveryOptimization\Cache\a\content.bin`, true, "缓存清理", ""},
		{`C:\pagefile.sys`, true, "虚拟内存", ""},
		{`D:\pagefile.sys`, true, "虚拟内存", ""},
		{`C:\hiberfil.sys`, true, "powercfg", ""},
		{`D:\System Volume Information\x`, true, "还原点", ""},
		{`E:\$RECYCLE.BIN\S-1-5\$R1.mp4`, true, "回收站", ""},
		{`C:\Users\u\NTUSER.DAT`, true, "注册表", ""},
		{`C:\Users\other\ntuser.dat`, true, "注册表", ""},
		{`C:\Users\Default\setup.txt`, true, "模板", ""},
		{`C:\Users\u\.ssh\id_ed25519`, true, "SSH", ""},
		{`C:\Windows.old\Windows\x.dll`, true, "以前的 Windows", ""},
		{`C:\Users\u\Videos\movie.mkv`, false, "", ""},
		{`D:\vm\ubuntu.vhdx`, false, "", "磁盘镜像"},
		{`C:\Users\u\Documents\Outlook\me.pst`, false, "", "Outlook"},
		// 微信、QQ 放在文档里的数据:能删,但删了聊天记录里的文件就打不开了
		{`C:\Users\u\Documents\WeChat Files\wxid_x\FileStorage\File\2026-01\报告.pdf`, false, "", "微信"},
		{`D:\Documents\xwechat_files\wxid_x\msg\video\a.mp4`, false, "", "微信"},
		{`C:\Users\u\Documents\Tencent Files\123\FileRecv\a.zip`, false, "", "QQ"},
		// 网盘同步目录:删了会同步到云端
		{`C:\Users\u\Documents\WPSDrive\309\合同.docx`, false, "", "云端"},
		{`D:\OneDrive - Contoso\报表.xlsx`, false, "", "云端"},
		// 系统盘以外的盘上,叫这些名字的目录是用户自己的数据,不能误伤
		{`D:\Recovery\phone.img`, false, "", ""},
		{`D:\Boot\notes.txt`, false, "", ""},
		{`relative\path`, true, "不完整", ""},
		{``, true, "不完整", ""},
	}
	for _, c := range cases {
		v := g.Check(c.path)
		if v.Blocked != c.blocked {
			t.Errorf("%q: Blocked = %v,应为 %v(原因:%s)", c.path, v.Blocked, c.blocked, v.Reason)
			continue
		}
		if c.reason != "" && !strings.Contains(v.Reason, c.reason) {
			t.Errorf("%q: 原因 %q 里没有 %q", c.path, v.Reason, c.reason)
		}
		if c.warn != "" && !strings.Contains(v.Warn, c.warn) {
			t.Errorf("%q: 提醒 %q 里没有 %q", c.path, v.Warn, c.warn)
		}
		if c.warn == "" && v.Warn != "" {
			t.Errorf("%q: 不该有提醒,却给了 %q", c.path, v.Warn)
		}
	}
}

func TestGuardCheckContents(t *testing.T) {
	g := newGuard(windowsSpec(fakeEnv()))
	cases := []struct {
		dir     string
		blocked bool
	}{
		{`C:\`, true},
		{`C:\Windows`, true},
		{`C:\Windows\System32`, true},
		{`C:\Windows\Temp`, false},
		{`C:\Windows\SoftwareDistribution\Download`, false},
		{`C:\Users`, true},
		{`C:\Users\u`, true},
		// 别人的家也一样
		{`C:\Users\other`, true},
		{`C:\Users\u\Documents`, true},
		{`C:\Users\u\Desktop`, true},
		{`C:\Users\u\AppData`, true},
		{`C:\Users\u\AppData\Local`, true},
		{`C:\Users\u\AppData\Roaming`, true},
		{`C:\Users\u\AppData\Local\Temp`, false},
		{`C:\Users\u\AppData\Local\Google\Chrome\User Data\Default\Cache`, false},
		{`C:\ProgramData`, true},
		{`C:\ProgramData\Microsoft\Windows\WER\ReportQueue`, false},
		{`C:\Program Files\App\cache`, true},
		// 一层深的目录不可能是谁的缓存目录:规则写错退化出来的往往就是这种
		{`D:\data`, true},
		{`D:\data\cache`, false},
		{`D:\$Recycle.Bin\S-1-5`, true},
	}
	for _, c := range cases {
		if v := g.CheckContents(c.dir); v.Blocked != c.blocked {
			t.Errorf("清空 %q: Blocked = %v,应为 %v(原因:%s)", c.dir, v.Blocked, c.blocked, v.Reason)
		}
	}
}

// 8.3 短文件名是字符串规则的盲区:C:\PROGRA~1 就是 C:\Program Files。
// 光比字符串会放行,CheckFinal 解到真实路径之后必须拦住
func TestCheckFinalResolvesShortNames(t *testing.T) {
	short := `C:\PROGRA~1`
	if _, err := os.Stat(short); err != nil {
		t.Skip("这台机器的系统盘没开短文件名")
	}
	g := NewGuard()
	p := short + `\Common Files`
	if _, err := os.Stat(p); err != nil {
		t.Skip("没有 Common Files 目录")
	}
	if v := g.CheckFinal(p); !v.Blocked {
		t.Fatalf("%s 实际是 Program Files 下面的目录,应该被拦住", p)
	}
}

func TestAppDirsAreProtected(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	g := NewGuard()
	if v := g.Check(home + `\.toolforge\hotkeys.json`); !v.Blocked {
		t.Fatal("工具箱自己的配置不该能被删")
	}
}
