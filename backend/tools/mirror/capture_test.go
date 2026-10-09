package mirror

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractPNG(t *testing.T) {
	img := append(append([]byte{}, pngMagic...), []byte("....IHDR....IEND\xae\x42\x60\x82")...)
	cases := []struct {
		name string
		raw  []byte
	}{
		{"干净的", img},
		{"前面有警告", append([]byte("[Warning] Multiple displays were found\n"), img...)},
		{"后面多了换行", append(append([]byte{}, img...), '\r', '\n')},
	}
	for _, c := range cases {
		got, err := extractPNG(c.raw)
		if err != nil || !bytes.Equal(got, img) {
			t.Errorf("%s: 取出 %q %v", c.name, got, err)
		}
	}
	// 根本没有图:把手机说的话带出来
	if _, err := extractPNG([]byte("screencap: permission denied")); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("没有图片时要说手机回了什么: %v", err)
	}
	if _, err := extractPNG(nil); err == nil {
		t.Error("空输出该报错")
	}
}

func TestCaptureName(t *testing.T) {
	at := time.Date(2026, 10, 9, 15, 30, 12, 0, time.Local)
	if got := captureName("GM1910", "截图", at); got != "GM1910_截图_20261009_153012" {
		t.Errorf("名字不对: %s", got)
	}
	// 型号里有 Windows 文件名不让用的字符
	if got := safeName(`a/b:c*"d"?`); got != "a_b_c__d__" {
		t.Errorf("没换掉非法字符: %s", got)
	}
	if got := safeName(" . "); got != "手机" {
		t.Errorf("空名字该有个兜底: %q", got)
	}
}

// 同名的已经在了:第二个加 _2,绝不覆盖
func TestWriteNewNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	a, err := writeNew(dir, "x", ".png", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := writeNew(dir, "x", ".png", []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(a) != "x.png" || filepath.Base(b) != "x_2.png" {
		t.Fatalf("名字不对: %s %s", a, b)
	}
	if data, _ := os.ReadFile(a); string(data) != "one" {
		t.Fatal("第一个被覆盖了")
	}
	if err := checkDir(filepath.Join(dir, "x.png")); err == nil {
		t.Error("文件不是文件夹,该报错")
	}
	if err := checkDir(""); err == nil {
		t.Error("没选文件夹该报错")
	}
}

// 转屏:画面尺寸变了,一个 MP4 装不下两种尺寸,另起一个文件接着录;
// 手机重起编码后时间戳从头算,录像的时间轴照样往前走
func TestRecorderSplitsOnRotation(t *testing.T) {
	dir := t.TempDir()
	r := newRecorder(filepath.Join(dir, "x"))
	clock := time.Unix(100, 0)
	r.now = func() time.Time { return clock }
	feed := func(p []byte) {
		t.Helper()
		if err := r.feed(p); err != nil {
			t.Fatal(err)
		}
	}

	// 关键帧来之前的普通帧丢掉:没有它们依赖的关键帧,解不出来
	feed(sessionPacket(540, 1200))
	feed(mediaPacket(1<<62, 0, testConfig))
	feed(mediaPacket(0, 5_000_000, testDeltaFrame))
	if len(r.files) != 0 {
		t.Fatal("没等到关键帧就建了文件")
	}
	for i := 0; i < 10; i++ {
		flags := uint64(0)
		if i == 0 {
			flags = 1 << 61
		}
		feed(mediaPacket(flags, 5_000_000+uint64(i)*33_333, testKeyFrame))
		clock = clock.Add(33 * time.Millisecond)
	}
	before := r.lastOut

	// 转屏:新尺寸、新的编码参数,手机端时间戳从 0 重新算
	clock = clock.Add(200 * time.Millisecond)
	feed(sessionPacket(1200, 540))
	feed(mediaPacket(1<<62, 0, []byte{0, 0, 0, 1, 0x67, 0x64, 0x00, 0x28, 0xad, 0, 0, 0, 1, 0x68, 0xee, 0x3c, 0x80}))
	feed(mediaPacket(1<<61, 0, testKeyFrame))
	if r.lastOut <= before+200_000-1 {
		t.Errorf("转屏后时间轴没接上: 之前 %d,之后 %d", before, r.lastOut)
	}
	for i := 1; i <= 5; i++ {
		feed(mediaPacket(0, uint64(i)*33_333, testDeltaFrame))
	}
	res := r.finish()
	if len(res.Files) != 2 || filepath.Base(res.Files[1]) != "x_2.mp4" || res.Error != "" {
		t.Fatalf("转屏该另起一个文件: %+v", res)
	}
	checkMP4(t, res.Files[0], 540, 1200, 10)
	checkMP4(t, res.Files[1], 1200, 540, 6)
	if res.DurationMs < 500 || res.Bytes == 0 {
		t.Errorf("结果不对: %+v", res)
	}
}

// 开了录屏还没等到关键帧就停了:不留空文件
func TestRecorderStoppedBeforeKeyFrame(t *testing.T) {
	dir := t.TempDir()
	r := newRecorder(filepath.Join(dir, "x"))
	if err := r.feed(sessionPacket(540, 1200)); err != nil {
		t.Fatal(err)
	}
	res := r.finish()
	if len(res.Files) != 0 {
		t.Fatalf("没录到画面却有文件: %+v", res)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("留下了文件: %v", entries)
	}
}
