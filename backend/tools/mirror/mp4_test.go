package mirror

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// 测试用的 H.264 片段:配置包里是 SPS(High 档次)和 PPS,后面一个关键帧、一个普通帧
var (
	testConfig     = []byte{0, 0, 0, 1, 0x67, 0x64, 0x00, 0x1f, 0xac, 0, 0, 0, 1, 0x68, 0xee, 0x3c, 0x80}
	testKeyFrame   = []byte{0, 0, 0, 1, 0x65, 0x88, 0x84, 0x21}
	testDeltaFrame = []byte{0, 0, 0, 1, 0x41, 0x9a, 0x02, 0x03}
)

func TestSplitNALs(t *testing.T) {
	// 四字节、三字节起始码混着来,前一个 NAL 末尾不能带上下一个起始码的 0
	b := []byte{0, 0, 0, 1, 0x09, 0xf0, 0, 0, 0, 1, 0x67, 0x64, 0x00, 0x1f, 0, 0, 1, 0x68, 0xee, 0, 0, 1, 0x65, 0x88}
	got := splitNALs(b)
	want := [][]byte{{0x09, 0xf0}, {0x67, 0x64, 0x00, 0x1f}, {0x68, 0xee}, {0x65, 0x88}}
	if len(got) != len(want) {
		t.Fatalf("切出 %d 个,应为 %d: % x", len(got), len(want), got)
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("第 %d 个: % x,应为 % x", i+1, got[i], want[i])
		}
	}
	// 帧里的分隔符、SPS、PPS 去掉,剩下的换成 4 字节长度开头
	if got := annexBToAVCC(b); !bytes.Equal(got, []byte{0, 0, 0, 2, 0x65, 0x88}) {
		t.Errorf("转成 MP4 写法不对: % x", got)
	}
	sps, pps := parameterSets(testConfig)
	if !bytes.Equal(sps, []byte{0x67, 0x64, 0x00, 0x1f, 0xac}) || !bytes.Equal(pps, []byte{0x68, 0xee, 0x3c, 0x80}) {
		t.Errorf("SPS/PPS 没取对: % x / % x", sps, pps)
	}
	if sps, pps := parameterSets([]byte{0, 0, 1, 0x65, 0x88}); sps != nil || pps != nil {
		t.Error("没有 SPS/PPS 的不该取出东西")
	}
}

// 写一个文件再一层层拆开:盒子嵌套、时长回填、每段的数据偏移都要对
func TestMP4File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	sps, pps := parameterSets(testConfig)
	m, err := createMP4(path, 540, 1200, sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	key, delta := annexBToAVCC(testKeyFrame), annexBToAVCC(testDeltaFrame)
	// 45 帧、每帧 1/30 秒:第一秒满了写一段,剩下的收尾时写第二段
	for i := 0; i < 45; i++ {
		data := delta
		if i == 0 {
			data = key
		}
		if err := m.add(sample{ticks: int64(i) * 3000, key: i == 0, data: data}); err != nil {
			t.Fatal(err)
		}
	}
	ms, err := m.close()
	if err != nil {
		t.Fatal(err)
	}
	if ms != 1500 {
		t.Errorf("时长 %d 毫秒,应为 1500", ms)
	}
	checkMP4(t, path, 540, 1200, 45)

	// 已经有的文件绝不覆盖
	if _, err := createMP4(path, 540, 1200, sps, pps); err == nil {
		t.Fatal("同名文件被覆盖了")
	}
}

// checkMP4 拆开一个录出来的文件,核对结构;返回各段的帧数
func checkMP4(t *testing.T, path string, width, height, frames int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	top := parseBoxes(t, data)
	if len(top) < 4 || top[0].typ != "ftyp" || top[1].typ != "moov" {
		t.Fatalf("开头该是 ftyp、moov,得到 %v", boxTypes(top))
	}
	if ftyp := top[0].body; string(ftyp[:4]) != "isom" || !bytes.Contains(ftyp, []byte("iso5")) {
		t.Errorf("ftyp 不对: %q", ftyp)
	}

	moov := parseBoxes(t, top[1].body)
	mvhd := find(t, moov, "mvhd").body
	movieMs := binary.BigEndian.Uint64(mvhd[24:])
	mehd := find(t, parseBoxes(t, find(t, moov, "mvex").body), "mehd").body
	if movieMs == 0 || binary.BigEndian.Uint64(mehd[4:]) != movieMs {
		t.Errorf("时长没回填: mvhd %d, mehd %d", movieMs, binary.BigEndian.Uint64(mehd[4:]))
	}
	trak := parseBoxes(t, find(t, moov, "trak").body)
	tkhd := find(t, trak, "tkhd").body
	if w, h := binary.BigEndian.Uint32(tkhd[len(tkhd)-8:]), binary.BigEndian.Uint32(tkhd[len(tkhd)-4:]); w != uint32(width)<<16 || h != uint32(height)<<16 {
		t.Errorf("画面尺寸不对: %d×%d", w>>16, h>>16)
	}
	mdia := parseBoxes(t, find(t, trak, "mdia").body)
	mdhd := find(t, mdia, "mdhd").body
	if binary.BigEndian.Uint32(mdhd[20:]) != mediaTimescale || binary.BigEndian.Uint64(mdhd[24:]) == 0 {
		t.Errorf("视频轨的时间单位或时长不对: % x", mdhd[20:32])
	}
	stbl := parseBoxes(t, find(t, parseBoxes(t, find(t, mdia, "minf").body), "stbl").body)
	stsd := find(t, stbl, "stsd").body
	avc1 := parseBoxes(t, stsd[8:])[0]
	if avc1.typ != "avc1" {
		t.Fatalf("样本描述该是 avc1,得到 %s", avc1.typ)
	}
	// 测试数据都是 High 档次(0x64),High 一系要多带 4 个字节
	avcC := parseBoxes(t, avc1.body[78:])[0]
	if avcC.typ != "avcC" || avcC.body[0] != 1 || avcC.body[1] != 0x64 || avcC.body[4] != 0xff || avcC.body[5] != 0xe1 {
		t.Errorf("avcC 不对: %s % x", avcC.typ, avcC.body)
	}
	spsLen := int(binary.BigEndian.Uint16(avcC.body[6:]))
	if len(avcC.body) != 6+2+spsLen+1+2+4+4 {
		t.Errorf("avcC 长度 %d 不对", len(avcC.body))
	}

	total, next := 0, uint64(0)
	for i := 2; i < len(top); i += 2 {
		if top[i].typ != "moof" || i+1 >= len(top) || top[i+1].typ != "mdat" {
			t.Fatalf("分段该是 moof、mdat 成对,得到 %v", boxTypes(top))
		}
		traf := parseBoxes(t, find(t, parseBoxes(t, top[i].body), "traf").body)
		base := binary.BigEndian.Uint64(find(t, traf, "tfdt").body[4:])
		if base != next {
			t.Errorf("第 %d 段从 %d 开始,上一段结束在 %d", i/2, base, next)
		}
		trun := find(t, traf, "trun").body
		n := int(binary.BigEndian.Uint32(trun[4:]))
		offset := int(binary.BigEndian.Uint32(trun[8:]))
		// 数据偏移从 moof 开头算,要正好落在 mdat 的数据上
		if offset != len(top[i].body)+8+8 {
			t.Errorf("第 %d 段的数据偏移 %d 不对", i/2, offset)
		}
		size := 0
		for j := 0; j < n; j++ {
			e := trun[12+j*12:]
			dur, sz, flags := binary.BigEndian.Uint32(e), binary.BigEndian.Uint32(e[4:]), binary.BigEndian.Uint32(e[8:])
			if dur == 0 {
				t.Errorf("第 %d 帧时长是 0", total+j+1)
			}
			if total+j == 0 && flags != 0x2000000 {
				t.Errorf("第一帧该标成关键帧: %#x", flags)
			}
			next += uint64(dur)
			size += int(sz)
		}
		if size != len(top[i+1].body) {
			t.Errorf("第 %d 段帧大小加起来 %d,mdat 有 %d", i/2, size, len(top[i+1].body))
		}
		total += n
	}
	if total != frames {
		t.Errorf("一共 %d 帧,应为 %d", total, frames)
	}
	if binary.BigEndian.Uint64(mdhd[24:]) != next {
		t.Errorf("视频轨时长 %d 和各帧加起来的 %d 对不上", binary.BigEndian.Uint64(mdhd[24:]), next)
	}
}

type mp4Box struct {
	typ  string
	body []byte
}

func parseBoxes(t *testing.T, b []byte) []mp4Box {
	t.Helper()
	var out []mp4Box
	for len(b) > 0 {
		if len(b) < 8 {
			t.Fatalf("剩下 %d 字节,不够一个盒子头", len(b))
		}
		size := int(binary.BigEndian.Uint32(b))
		if size < 8 || size > len(b) {
			t.Fatalf("%q 盒子长度 %d 不对(还剩 %d)", b[4:8], size, len(b))
		}
		out = append(out, mp4Box{string(b[4:8]), b[8:size]})
		b = b[size:]
	}
	return out
}

func find(t *testing.T, boxes []mp4Box, typ string) mp4Box {
	t.Helper()
	for _, b := range boxes {
		if b.typ == typ {
			return b
		}
	}
	t.Fatalf("没有 %s,只有 %v", typ, boxTypes(boxes))
	return mp4Box{}
}

func boxTypes(boxes []mp4Box) []string {
	var out []string
	for _, b := range boxes {
		out = append(out, b.typ)
	}
	return out
}
