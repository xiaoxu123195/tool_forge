package diskclean

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
	"unicode/utf16"
)

// lnkSpec 拼一个快捷方式要的东西,只覆盖解析器关心的部分
type lnkSpec struct {
	idList     []byte
	ansiBase   string // LinkInfo 里系统代码页的路径
	ansiSuffix string
	uniBase    string // LinkInfo 里 Unicode 的路径(头长 0x24)
	driveType  uint32
	network    bool
	relPath    string
	envTarget  string
	darwin     bool
}

func u16le(s string) []byte {
	var b bytes.Buffer
	for _, c := range utf16.Encode([]rune(s)) {
		binary.Write(&b, binary.LittleEndian, c)
	}
	return b.Bytes()
}

func buildLnk(sp lnkSpec) []byte {
	var b bytes.Buffer
	w32 := func(v uint32) { binary.Write(&b, binary.LittleEndian, v) }
	w16 := func(v uint16) { binary.Write(&b, binary.LittleEndian, v) }

	flags := uint32(lnkIsUnicode)
	if sp.idList != nil {
		flags |= lnkHasIDList
	}
	hasLI := sp.ansiBase != "" || sp.uniBase != "" || sp.network
	if hasLI {
		flags |= lnkHasLinkInfo
	}
	if sp.relPath != "" {
		flags |= lnkHasRelPath
	}
	if sp.darwin {
		flags |= lnkHasDarwinID
	}
	w32(0x4C)
	b.Write(lnkHeaderCLSID)
	w32(flags)
	b.Write(make([]byte, 0x4C-b.Len()))

	if sp.idList != nil {
		w16(uint16(len(sp.idList)))
		b.Write(sp.idList)
	}
	if hasLI {
		headerSize := uint32(0x1C)
		if sp.uniBase != "" {
			headerSize = 0x24
		}
		var body bytes.Buffer
		liFlags := uint32(0)
		var volOff, baseOff, suffixOff, uniBaseOff, uniSuffixOff uint32
		at := func() uint32 { return headerSize + uint32(body.Len()) }
		if sp.ansiBase != "" || sp.uniBase != "" {
			liFlags |= linkInfoLocal
			volOff = at()
			binary.Write(&body, binary.LittleEndian, uint32(0x11)) // VolumeIDSize
			binary.Write(&body, binary.LittleEndian, sp.driveType)
			binary.Write(&body, binary.LittleEndian, uint32(0x1234)) // 序列号
			binary.Write(&body, binary.LittleEndian, uint32(0x10))   // 卷标偏移
			body.WriteByte(0)
			baseOff = at()
			body.WriteString(sp.ansiBase)
			body.WriteByte(0)
			suffixOff = at()
			body.WriteString(sp.ansiSuffix)
			body.WriteByte(0)
			if sp.uniBase != "" {
				uniBaseOff = at()
				body.Write(u16le(sp.uniBase))
				body.Write([]byte{0, 0})
				uniSuffixOff = at()
				body.Write([]byte{0, 0})
			}
		}
		if sp.network {
			liFlags |= linkInfoNetwork
		}
		li := &bytes.Buffer{}
		lw := func(v uint32) { binary.Write(li, binary.LittleEndian, v) }
		lw(headerSize + uint32(body.Len()))
		lw(headerSize)
		lw(liFlags)
		lw(volOff)
		lw(baseOff)
		lw(0)
		lw(suffixOff)
		if headerSize >= 0x24 {
			lw(uniBaseOff)
			lw(uniSuffixOff)
		}
		li.Write(body.Bytes())
		b.Write(li.Bytes())
	}
	if sp.relPath != "" {
		w16(uint16(len(utf16.Encode([]rune(sp.relPath)))))
		b.Write(u16le(sp.relPath))
	}
	if sp.envTarget != "" {
		w32(0x314)
		w32(sigEnvironmentBlock)
		ansi := make([]byte, 260)
		copy(ansi, sp.envTarget)
		b.Write(ansi)
		uni := make([]byte, 520)
		copy(uni, u16le(sp.envTarget))
		b.Write(uni)
	}
	if sp.darwin {
		w32(0x314)
		w32(sigDarwinBlock)
		b.Write(make([]byte, 0x314-8))
	}
	w32(0) // 结束块
	return b.Bytes()
}

func ascii(b []byte) string { return string(b) }

func TestParseLnk(t *testing.T) {
	cases := []struct {
		name string
		sp   lnkSpec
		want lnkInfo
	}{
		{"系统代码页路径", lnkSpec{idList: []byte{2, 0}, ansiBase: `C:\Tools\app.exe`, driveType: 3},
			lnkInfo{localPath: `C:\Tools\app.exe`, driveType: 3}},
		{"路径分两截存", lnkSpec{ansiBase: `C:\Tools`, ansiSuffix: `app.exe`, driveType: 3},
			lnkInfo{localPath: `C:\Tools\app.exe`, driveType: 3}},
		{"Unicode 路径优先", lnkSpec{ansiBase: `C:\????\??.exe`, uniBase: `C:\工具\应用.exe`, driveType: 3},
			lnkInfo{localPath: `C:\工具\应用.exe`, driveType: 3}},
		{"U 盘上的", lnkSpec{ansiBase: `E:\x.exe`, driveType: 2},
			lnkInfo{localPath: `E:\x.exe`, driveType: 2}},
		{"环境变量", lnkSpec{envTarget: `%windir%\notepad.exe`},
			lnkInfo{envPath: `%windir%\notepad.exe`}},
		{"相对路径", lnkSpec{relPath: `..\bin\tool.exe`},
			lnkInfo{relPath: `..\bin\tool.exe`}},
		{"安装器的广告快捷方式", lnkSpec{idList: []byte{2, 0}, darwin: true},
			lnkInfo{darwin: true}},
		{"网络位置", lnkSpec{network: true},
			lnkInfo{network: true}},
	}
	for _, c := range cases {
		got, err := parseLnk(buildLnk(c.sp), ascii)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		cmp := *got
		cmp.idList = nil
		if !reflect.DeepEqual(cmp, c.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", c.name, cmp, c.want)
		}
	}
}

func TestParseLnkIDList(t *testing.T) {
	// 两项再加结尾:每项是"长度 + 内容"
	idl := []byte{5, 0, 0x1F, 0x50, 0x01, 4, 0, 0x2F, 0x43, 0, 0}
	got, err := parseLnk(buildLnk(lnkSpec{idList: idl}), ascii)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.idList, idl) {
		t.Fatalf("项目标识列表没原样取出来:%v", got.idList)
	}
	// 结构不对的不交给系统去转
	for _, bad := range [][]byte{
		{2, 0},                // 只有一个空项
		{9, 0, 1, 2, 0, 0},    // 项的长度越界
		{5, 0, 1, 2, 3},       // 没有结尾
		{4, 0, 1, 2, 0, 0, 7}, // 结尾后面还有东西
	} {
		if validIDList(bad) {
			t.Errorf("坏的项目标识列表被当成好的:%v", bad)
		}
	}
}

// 快捷方式文件可能是坏的、截断的、故意构造的:截在任何一个字节上都不能让程序崩
func TestParseLnkNeverPanics(t *testing.T) {
	full := buildLnk(lnkSpec{idList: []byte{4, 0, 1, 2}, uniBase: `C:\工具\应用.exe`, ansiBase: "x",
		driveType: 3, relPath: `..\a`, envTarget: `%x%\y`, darwin: true})
	for i := 0; i <= len(full); i++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("截到第 %d 字节时崩了:%v", i, r)
				}
			}()
			_, _ = parseLnk(full[:i], ascii)
		}()
	}
	// 头对、但 LinkInfo 长度胡写
	bad := buildLnk(lnkSpec{ansiBase: `C:\a.exe`, driveType: 3})
	binary.LittleEndian.PutUint32(bad[0x4C:], 0xFFFFFFF0)
	if _, err := parseLnk(bad, ascii); err == nil {
		t.Error("LinkInfo 长度越界应该报错")
	}
	if _, err := parseLnk([]byte("not a shortcut at all, just some text..........................................."), ascii); err == nil {
		t.Error("不是快捷方式的文件应该报错")
	}
}
