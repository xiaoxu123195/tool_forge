package mirror

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// 浏览器录出来的 MP4 补时长。
//
// 浏览器录屏是边录边出的分片 MP4:开头的 moov 写下来的时候还不知道要录多久,
// 总时长填的是 0,视频轨只记了最开始那一小段,也没有记分片总长的 mehd。
// Windows 自带的播放器拿这些当真:时长显示不对、进度条拖不动。
// 录完以后把各分片的时长加起来填回去,没有 mehd 就补一个 —— 和安卓录屏自己写的 MP4 一样齐全

// finishRecording 录像文件收尾:MP4 补时长,WebM 原样留着
func finishRecording(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".mp4") {
		return nil
	}
	if err := fixMP4Duration(path); err != nil {
		return fmt.Errorf("录像能播,但补时长失败了,播放器里的进度条可能不对:%w", err)
	}
	return nil
}

var errBadBox = errors.New("MP4 结构不完整")

// isoBox 一个盒子:off 起点,hdr 头长(8,大盒子 16),size 连头在内的总长
type isoBox struct {
	typ            string
	off, hdr, size int64
}

func (b isoBox) body() int64 { return b.off + b.hdr }
func (b isoBox) end() int64  { return b.off + b.size }

// boxAt 读 off 处的盒子头。end 是这一层的结尾,size 为 0 的盒子一直到这里
func boxAt(r io.ReaderAt, off, end int64) (isoBox, error) {
	if end-off < 8 {
		return isoBox{}, errBadBox
	}
	var h [16]byte
	if _, err := r.ReadAt(h[:8], off); err != nil {
		return isoBox{}, err
	}
	b := isoBox{typ: string(h[4:8]), off: off, hdr: 8, size: int64(binary.BigEndian.Uint32(h[:4]))}
	switch b.size {
	case 1:
		if _, err := r.ReadAt(h[8:16], off+8); err != nil {
			return isoBox{}, err
		}
		b.size, b.hdr = int64(binary.BigEndian.Uint64(h[8:16])), 16
	case 0:
		b.size = end - off
	}
	if b.size < b.hdr || b.end() > end {
		return isoBox{}, errBadBox
	}
	return b, nil
}

// children 一个盒子里面的下一层
func children(r io.ReaderAt, parent isoBox) ([]isoBox, error) {
	var out []isoBox
	for p := parent.body(); p < parent.end(); {
		b, err := boxAt(r, p, parent.end())
		if err != nil {
			return out, err
		}
		out = append(out, b)
		p = b.end()
	}
	return out, nil
}

func findBox(bs []isoBox, typ string) (isoBox, bool) {
	for _, b := range bs {
		if b.typ == typ {
			return b, true
		}
	}
	return isoBox{}, false
}

func fixMP4Duration(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	top := isoBox{off: 0, hdr: 0, size: st.Size()}
	var moov isoBox
	var moofs []isoBox
	// 最后一个分片可能没写完(录着录着程序被关了):读到坏的就停,前面的照样算
	for p := int64(0); p < top.size; {
		b, err := boxAt(f, p, top.size)
		if err != nil {
			break
		}
		switch b.typ {
		case "moov":
			moov = b
		case "moof":
			moofs = append(moofs, b)
		}
		p = b.end()
	}
	if moov.typ == "" {
		f.Close()
		return errors.New("没有 moov")
	}
	moovData := make([]byte, moov.size)
	if _, err := f.ReadAt(moovData, moov.off); err != nil {
		f.Close()
		return err
	}
	trex, err := trexDurations(moovData)
	if err != nil {
		f.Close()
		return err
	}
	ends := map[uint32]uint64{}
	absolute := false
	for _, m := range moofs {
		data := make([]byte, m.size)
		if _, err := f.ReadAt(data, m.off); err != nil {
			break
		}
		abs, err := fragmentEnds(data, trex, ends)
		if err != nil {
			break
		}
		absolute = absolute || abs
	}
	// 分片里的数据位置都是相对各自 moof 的,moov 变长、整体往后挪不碍事;
	// 有按文件绝对位置记的就只能原地改,不补 mehd
	patched, err := patchMoov(moovData, ends, !absolute)
	if err != nil {
		f.Close()
		return err
	}
	if len(patched) == len(moovData) {
		_, err = f.WriteAt(patched, moov.off)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}
	f.Close()
	return rewriteMoov(path, moov, patched)
}

// rewriteMoov moov 变长了:换着 moov 另写一份,再替换原文件
func rewriteMoov(path string, moov isoBox, patched []byte) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := path + ".tmp"
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, io.NewSectionReader(src, 0, moov.off))
	if err == nil {
		_, err = dst.Write(patched)
	}
	if err == nil {
		_, err = src.Seek(moov.end(), io.SeekStart)
	}
	if err == nil {
		_, err = io.Copy(dst, src)
	}
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	src.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// trexDurations moov/mvex 里每条轨道的默认样本时长:分片里没写时长的样本按它算
func trexDurations(moov []byte) (map[uint32]uint32, error) {
	r := bytes.NewReader(moov)
	out := map[uint32]uint32{}
	kids, err := children(r, isoBox{typ: "moov", size: int64(len(moov)), hdr: 8})
	if err != nil {
		return nil, err
	}
	mvex, ok := findBox(kids, "mvex")
	if !ok {
		return out, nil
	}
	ex, err := children(r, mvex)
	if err != nil {
		return nil, err
	}
	for _, b := range ex {
		if b.typ == "trex" && b.size >= b.hdr+20 {
			p := b.body()
			out[binary.BigEndian.Uint32(moov[p+4:])] = binary.BigEndian.Uint32(moov[p+12:])
		}
	}
	return out, nil
}

// fragmentEnds 一个 moof 里每条轨道播到哪儿(媒体时间单位),取大的记进 ends。
// 返回这个分片有没有按文件绝对位置记数据(tfhd 的 base-data-offset)
func fragmentEnds(moof []byte, trex map[uint32]uint32, ends map[uint32]uint64) (bool, error) {
	r := bytes.NewReader(moof)
	kids, err := children(r, isoBox{typ: "moof", size: int64(len(moof)), hdr: 8})
	if err != nil {
		return false, err
	}
	absolute := false
	u32 := func(p int64) uint32 { return binary.BigEndian.Uint32(moof[p:]) }
	for _, traf := range kids {
		if traf.typ != "traf" {
			continue
		}
		parts, err := children(r, traf)
		if err != nil {
			return absolute, err
		}
		var track, defDur uint32
		var base, sum uint64
		haveBase := false
		for _, b := range parts {
			p, end := b.body(), b.end()
			switch b.typ {
			case "tfhd":
				if end-p < 8 {
					return absolute, errBadBox
				}
				flags := u32(p) & 0xffffff
				track = u32(p + 4)
				defDur = trex[track]
				q := p + 8
				if flags&0x01 != 0 {
					absolute = true
					q += 8
				}
				if flags&0x02 != 0 {
					q += 4
				}
				if flags&0x08 != 0 && q+4 <= end {
					defDur = u32(q)
				}
			case "tfdt":
				if end-p < 8 {
					return absolute, errBadBox
				}
				haveBase = true
				if moof[p] == 1 && end-p >= 12 {
					base = binary.BigEndian.Uint64(moof[p+4:])
				} else {
					base = uint64(u32(p + 4))
				}
			case "trun":
				if end-p < 8 {
					return absolute, errBadBox
				}
				flags := u32(p) & 0xffffff
				n := int64(u32(p + 4))
				q := p + 8
				if flags&0x01 != 0 {
					q += 4
				}
				if flags&0x04 != 0 {
					q += 4
				}
				per := int64(0)
				for _, bit := range []uint32{0x100, 0x200, 0x400, 0x800} {
					if flags&bit != 0 {
						per += 4
					}
				}
				if q+n*per > end {
					return absolute, errBadBox
				}
				for i := int64(0); i < n; i++ {
					d := defDur
					if flags&0x100 != 0 {
						d = u32(q + i*per)
					}
					sum += uint64(d)
				}
			}
		}
		if !haveBase {
			// 没有 tfdt:接着这条轨道上一个分片往下排
			base = ends[track]
		}
		if e := base + sum; e > ends[track] {
			ends[track] = e
		}
	}
	return absolute, nil
}

// patchMoov 把时长填进 mvhd、各轨的 tkhd 和 mdhd、mvex 里的 mehd。
// grow 为真时允许补一个 mehd 进去(moov 会变长)
func patchMoov(moov []byte, ends map[uint32]uint64, grow bool) ([]byte, error) {
	out := append([]byte(nil), moov...)
	r := bytes.NewReader(moov)
	root := isoBox{typ: "moov", size: int64(len(moov)), hdr: 8}
	kids, err := children(r, root)
	if err != nil {
		return nil, err
	}
	mvhd, ok := findBox(kids, "mvhd")
	if !ok || !fullBox(moov, mvhd, 20, 32) {
		return nil, errors.New("没有 mvhd")
	}
	movieScale := u32Field(moov, mvhd.body(), 12, 20)
	if movieScale == 0 {
		return nil, errors.New("mvhd 的时间单位是 0")
	}
	var longest uint64
	for _, trak := range kids {
		if trak.typ != "trak" {
			continue
		}
		tk, err := children(r, trak)
		if err != nil {
			return nil, err
		}
		tkhd, ok1 := findBox(tk, "tkhd")
		mdia, ok2 := findBox(tk, "mdia")
		if !ok1 || !ok2 {
			continue
		}
		md, err := children(r, mdia)
		if err != nil {
			return nil, err
		}
		mdhd, ok := findBox(md, "mdhd")
		if !ok || !fullBox(moov, tkhd, 24, 36) || !fullBox(moov, mdhd, 20, 32) {
			continue
		}
		track := uint32(u32Field(moov, tkhd.body(), 12, 20))
		media, ok := ends[track]
		if !ok {
			continue
		}
		mediaScale := u32Field(moov, mdhd.body(), 12, 20)
		if mediaScale == 0 {
			continue
		}
		movie := media * movieScale / mediaScale
		writeField(out, mdhd.body(), 16, 24, media)
		writeField(out, tkhd.body(), 20, 28, movie)
		longest = max(longest, movie)
	}
	if longest == 0 {
		return nil, errors.New("分片里没有时长")
	}
	writeField(out, mvhd.body(), 16, 24, longest)

	mvex, ok := findBox(kids, "mvex")
	if !ok {
		return out, nil
	}
	ex, err := children(r, mvex)
	if err != nil {
		return nil, err
	}
	if mehd, ok := findBox(ex, "mehd"); ok {
		if fullBox(moov, mehd, 8, 12) {
			writeField(out, mehd.body(), 4, 4, longest)
		}
		return out, nil
	}
	if !grow {
		return out, nil
	}
	// 补一个 mehd(64 位版本),放在 mvex 的最前面
	mehd := make([]byte, 20)
	binary.BigEndian.PutUint32(mehd, 20)
	copy(mehd[4:], "mehd")
	mehd[8] = 1
	binary.BigEndian.PutUint64(mehd[12:], longest)
	at := mvex.body()
	grown := make([]byte, 0, len(out)+len(mehd))
	grown = append(grown, out[:at]...)
	grown = append(grown, mehd...)
	grown = append(grown, out[at:]...)
	if err := addSize(grown, mvex, len(mehd)); err != nil {
		return nil, err
	}
	if err := addSize(grown, root, len(mehd)); err != nil {
		return nil, err
	}
	return grown, nil
}

// fullBox 盒子里的字段够不够长:版本 0 至少 n0 字节,版本 1 至少 n1 字节
func fullBox(b []byte, box isoBox, n0, n1 int64) bool {
	n := box.size - box.hdr
	if n < 1 {
		return false
	}
	if b[box.body()] == 1 {
		return n >= n1
	}
	return n >= n0
}

// u32Field 读 FullBox 里一个两个版本都是 32 位的字段(时间单位、轨道号):
// 版本 0 在 off0,版本 1 前面的时间戳变成 64 位,挪到 off1
func u32Field(b []byte, body int64, off0, off1 int64) uint64 {
	if b[body] == 1 {
		return uint64(binary.BigEndian.Uint32(b[body+off1:]))
	}
	return uint64(binary.BigEndian.Uint32(b[body+off0:]))
}

// writeField 写 FullBox 里的时长:版本 1 是 64 位,版本 0 是 32 位(放不下就封顶)
func writeField(b []byte, body int64, off0, off1 int64, v uint64) {
	if b[body] == 1 {
		binary.BigEndian.PutUint64(b[body+off1:], v)
		return
	}
	binary.BigEndian.PutUint32(b[body+off0:], uint32(min(v, 0xffffffff)))
}

// addSize 盒子变长了 n 字节:改它头里的长度
func addSize(b []byte, box isoBox, n int) error {
	if box.hdr == 16 {
		binary.BigEndian.PutUint64(b[box.off+8:], uint64(box.size)+uint64(n))
		return nil
	}
	size := uint64(box.size) + uint64(n)
	if size > 0xffffffff {
		return errors.New("moov 太大了")
	}
	binary.BigEndian.PutUint32(b[box.off:], uint32(size))
	return nil
}
