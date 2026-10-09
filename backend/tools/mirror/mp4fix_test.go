package mirror

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 造一个和浏览器录屏一样的分片 MP4:moov 里总时长是 0、轨道时长只记了开头,没有 mehd;
// 两个分片,样本时长都写在 trun 里

func isoPack(typ string, parts ...[]byte) []byte {
	n := 8
	for _, p := range parts {
		n += len(p)
	}
	b := make([]byte, 8, n)
	binary.BigEndian.PutUint32(b, uint32(n))
	copy(b[4:], typ)
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// fields 依次写 uint8、uint32、uint64 和字节串
func fields(vs ...any) []byte {
	var b bytes.Buffer
	for _, v := range vs {
		switch x := v.(type) {
		case uint8:
			b.WriteByte(x)
		case uint32:
			_ = binary.Write(&b, binary.BigEndian, x)
		case uint64:
			_ = binary.Write(&b, binary.BigEndian, x)
		case []byte:
			b.Write(x)
		}
	}
	return b.Bytes()
}

// vf FullBox 开头的版本和标志
func vf(version uint8, flags uint32) []byte {
	return fields(uint32(version)<<24 | flags)
}

func browserMoov(withMehd bool) []byte {
	mvhd := isoPack("mvhd", vf(1, 0), fields(uint64(0), uint64(0), uint32(1000), uint64(0), make([]byte, 80)))
	tkhd := isoPack("tkhd", vf(1, 3), fields(uint64(0), uint64(0), uint32(1), uint32(0), uint64(123), make([]byte, 60)))
	mdhd := isoPack("mdhd", vf(1, 0), fields(uint64(0), uint64(0), uint32(30000), uint64(99), uint32(0)))
	trak := isoPack("trak", tkhd, isoPack("mdia", mdhd))
	trex := isoPack("trex", vf(0, 0), fields(uint32(1), uint32(1), uint32(0), uint32(0), uint32(0)))
	mvex := isoPack("mvex", trex)
	if withMehd {
		mvex = isoPack("mvex", isoPack("mehd", vf(0, 0), fields(uint32(7))), trex)
	}
	return isoPack("moov", mvhd, trak, mvex)
}

// browserFragment 一个分片:tfdt 给起点,每个样本的时长写在 trun 里
func browserFragment(seq uint32, base uint64, durs []uint32, tfhdFlags uint32) []byte {
	tfhd := isoPack("tfhd", vf(0, tfhdFlags), fields(uint32(1)))
	if tfhdFlags&0x01 != 0 {
		tfhd = isoPack("tfhd", vf(0, tfhdFlags), fields(uint32(1), uint64(0)))
	}
	run := fields(uint32(len(durs)), uint32(0))
	for _, d := range durs {
		run = append(run, fields(d, uint32(4))...)
	}
	trun := isoPack("trun", vf(0, 0x301), run)
	moof := isoPack("moof", isoPack("mfhd", vf(0, 0), fields(seq)),
		isoPack("traf", tfhd, isoPack("tfdt", vf(1, 0), fields(base)), trun))
	return append(moof, isoPack("mdat", bytes.Repeat([]byte{byte(seq)}, 4*len(durs)))...)
}

func writeBrowserMP4(t *testing.T, withMehd bool, tfhdFlags uint32) (string, []byte) {
	t.Helper()
	ftyp := isoPack("ftyp", []byte("isom"), fields(uint32(0)), []byte("isomavc1"))
	rest := append(browserFragment(1, 0, []uint32{1000, 1000, 1000}, tfhdFlags), browserFragment(2, 3000, []uint32{1000, 1000}, tfhdFlags)...)
	file := append(append(ftyp, browserMoov(withMehd)...), rest...)
	path := filepath.Join(t.TempDir(), "rec.mp4")
	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, rest
}

// durations 读出修好之后的 mvhd、tkhd、mdhd、mehd 里的时长
func durations(t *testing.T, data []byte) (mvhd, tkhd, mdhd, mehd uint64) {
	t.Helper()
	r := bytes.NewReader(data)
	// 截断的文件最后一个盒子是坏的,前面读到的照样用
	top, _ := children(r, isoBox{size: int64(len(data))})
	moov, ok := findBox(top, "moov")
	if !ok {
		t.Fatal("没有 moov")
	}
	kids, _ := children(r, moov)
	b, _ := findBox(kids, "mvhd")
	mvhd = binary.BigEndian.Uint64(data[b.body()+24:])
	trak, _ := findBox(kids, "trak")
	tk, _ := children(r, trak)
	b, _ = findBox(tk, "tkhd")
	tkhd = binary.BigEndian.Uint64(data[b.body()+28:])
	mdia, _ := findBox(tk, "mdia")
	md, _ := children(r, mdia)
	b, _ = findBox(md, "mdhd")
	mdhd = binary.BigEndian.Uint64(data[b.body()+24:])
	mvex, _ := findBox(kids, "mvex")
	ex, _ := children(r, mvex)
	if b, ok := findBox(ex, "mehd"); ok {
		if data[b.body()] == 1 {
			mehd = binary.BigEndian.Uint64(data[b.body()+4:])
		} else {
			mehd = uint64(binary.BigEndian.Uint32(data[b.body()+4:]))
		}
	}
	return
}

func TestFixMP4DurationAddsMehd(t *testing.T) {
	path, rest := writeBrowserMP4(t, false, 0x020000)
	before, _ := os.ReadFile(path)
	if err := fixMP4Duration(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	// 5 个样本 × 1000,时间单位 30000 → 5000;电影时间单位 1000 → 166 毫秒
	mvhd, tkhd, mdhd, mehd := durations(t, after)
	if mdhd != 5000 || tkhd != 166 || mvhd != 166 || mehd != 166 {
		t.Fatalf("时长不对: mvhd=%d tkhd=%d mdhd=%d mehd=%d", mvhd, tkhd, mdhd, mehd)
	}
	if len(after) != len(before)+20 || !bytes.HasSuffix(after, rest) {
		t.Fatal("补 mehd 只该让 moov 变长,后面的分片一个字节都不能动")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("临时文件没清掉")
	}
}

func TestFixMP4DurationInPlace(t *testing.T) {
	path, _ := writeBrowserMP4(t, true, 0x020000)
	before, _ := os.ReadFile(path)
	if err := fixMP4Duration(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if len(after) != len(before) {
		t.Fatal("已经有 mehd 时应该原地改")
	}
	if mvhd, _, mdhd, mehd := durations(t, after); mvhd != 166 || mdhd != 5000 || mehd != 166 {
		t.Fatalf("时长不对: mvhd=%d mdhd=%d mehd=%d", mvhd, mdhd, mehd)
	}
}

func TestFixMP4DurationKeepsAbsoluteOffsets(t *testing.T) {
	// 分片按文件绝对位置记数据:moov 不能变长,只原地填时长
	path, _ := writeBrowserMP4(t, false, 0x01)
	before, _ := os.ReadFile(path)
	if err := fixMP4Duration(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if len(after) != len(before) {
		t.Fatal("按绝对位置记数据的文件不能挪动分片")
	}
	if mvhd, _, mdhd, mehd := durations(t, after); mvhd != 166 || mdhd != 5000 || mehd != 0 {
		t.Fatalf("时长不对: mvhd=%d mdhd=%d mehd=%d", mvhd, mdhd, mehd)
	}
}

func TestFixMP4DurationTruncatedTail(t *testing.T) {
	// 录着录着程序被关了:最后一个分片只写了一半,前面的照样算
	path, _ := writeBrowserMP4(t, false, 0x020000)
	data, _ := os.ReadFile(path)
	cut := data[:len(data)-30]
	_ = os.WriteFile(path, cut, 0o644)
	if err := fixMP4Duration(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if _, _, mdhd, _ := durations(t, after); mdhd != 3000 {
		t.Fatalf("只该算完整的那个分片: %d", mdhd)
	}
}

func TestUploadRecording(t *testing.T) {
	src, _ := writeBrowserMP4(t, false, 0x020000)
	data, _ := os.ReadFile(src)
	dir := t.TempDir()
	svc := New()

	if _, _, err := svc.BeginUpload(dir, "iPhone 8 Plus", ".avi"); err == nil {
		t.Fatal("不认识的格式应该拒绝")
	}
	id, path, err := svc.BeginUpload(dir, "iPhone 8 Plus", ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir || filepath.Ext(path) != ".mp4" {
		t.Fatalf("路径 %s", path)
	}
	half := len(data) / 2
	if err := svc.AppendUpload(id, data[:half]); err != nil {
		t.Fatal(err)
	}
	if err := svc.AppendUpload(id, data[half:]); err != nil {
		t.Fatal(err)
	}
	rec, err := svc.EndUpload(id, 1500)
	if err != nil || rec.Error != "" || len(rec.Files) != 1 || rec.Files[0] != path || rec.DurationMs != 1500 {
		t.Fatalf("结果不对: %+v %v", rec, err)
	}
	fixed, _ := os.ReadFile(path)
	if mvhd, _, _, _ := durations(t, fixed); mvhd != 166 {
		t.Fatalf("存下来的录像没补时长: %d", mvhd)
	}
	if err := svc.AppendUpload(id, data); err == nil {
		t.Fatal("结束之后不能再往里写")
	}

	// 一段都没传过来就停了:不留空文件
	id, path, _ = svc.BeginUpload(dir, "iPhone 8 Plus", ".mp4")
	rec, err = svc.EndUpload(id, 0)
	if err != nil || rec.Error == "" || len(rec.Files) != 0 {
		t.Fatalf("空录像应该报没录到: %+v %v", rec, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("空录像文件应该删掉")
	}
}

func TestSaveShot(t *testing.T) {
	dir := t.TempDir()
	svc := New()
	shot, err := svc.SaveShot(dir, "iPhone 8 Plus", tinyPNG(t, 3, 5))
	if err != nil {
		t.Fatal(err)
	}
	if shot.Width != 3 || shot.Height != 5 || filepath.Dir(shot.Path) != dir {
		t.Fatalf("截图 %+v", shot)
	}
	name := filepath.Base(shot.Path)
	if want := "iPhone 8 Plus_截图_" + time.Now().Format("20060102"); len(name) < len(want) || name[:len(want)] != want {
		t.Fatalf("文件名 %s", name)
	}
	if _, err := svc.SaveShot(dir, "iPhone 8 Plus", []byte("not png")); err == nil {
		t.Fatal("不是 PNG 应该拒绝")
	}
}

func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
