package mirror

import (
	"encoding/binary"
	"os"
)

// 录屏存成分片 MP4(fragmented MP4):文件头只写一次,之后每攒够一秒追加一段。
//
// 选分片格式不是图省事:普通 MP4 的索引要录完才写得出来,录到一半程序崩了、电脑断电,
// 整个文件就打不开了;分片的话最多丢最后一秒。手机编出来的 H.264 原样放进去,不重新编码。
// 盒子的字节布局照 ISO/IEC 14496-12(MP4 容器)和 14496-15(H.264 在 MP4 里的存法)。

const (
	// mediaTimescale 视频轨的时间单位:90kHz,视频文件里最常见的那个
	mediaTimescale = 90000
	// movieTimescale 整个文件的时长单位:毫秒
	movieTimescale = 1000
	// fragmentTicks 攒够这么长写一段
	fragmentTicks = mediaTimescale
)

// sample 一帧
type sample struct {
	// ticks 在这个文件里的时间,90kHz
	ticks int64
	dur   uint32
	key   bool
	// data 每个 NAL 前面是 4 字节长度(MP4 里的写法,不是起始码)
	data []byte
}

// mp4File 正在写的一个 MP4 文件
type mp4File struct {
	f    *os.File
	path string
	size int64
	seq  uint32
	// pending 最后一帧:下一帧来了才知道它有多长,所以总是晚一帧落盘
	pending   *sample
	frag      []sample
	fragTicks int64
	lastDur   uint32
	endTicks  int64
	// durs 文件头里几个时长字段的位置。写头时还不知道多长,收尾时回填
	durs []durField
}

type durField struct {
	off int64
	// ms 这个字段按毫秒记;否则按视频轨的 90kHz
	ms bool
}

// createMP4 建文件、写文件头。已经有同名文件时报错,绝不覆盖
func createMP4(path string, width, height int, sps, pps []byte) (*mp4File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	w := initSegment(width, height, sps, pps)
	if _, err := f.Write(w.b); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return &mp4File{f: f, path: path, size: int64(len(w.b)), durs: w.durs}, nil
}

// add 加一帧。帧的时间必须往前走,调用方保证
func (m *mp4File) add(s sample) error {
	if p := m.pending; p != nil {
		d := s.ticks - p.ticks
		if d < 1 {
			d = 1
		}
		p.dur = uint32(min(d, 1<<31))
		m.lastDur = p.dur
		m.frag = append(m.frag, *p)
		m.fragTicks += int64(p.dur)
		if m.fragTicks >= fragmentTicks {
			if err := m.flush(); err != nil {
				return err
			}
		}
	}
	m.pending = &s
	return nil
}

// flush 把攒着的帧写成一段
func (m *mp4File) flush() error {
	if len(m.frag) == 0 {
		return nil
	}
	m.seq++
	b := fragment(m.seq, m.frag[0].ticks, m.frag)
	if _, err := m.f.Write(b); err != nil {
		return err
	}
	last := m.frag[len(m.frag)-1]
	m.size += int64(len(b))
	m.endTicks = last.ticks + int64(last.dur)
	m.frag = nil
	m.fragTicks = 0
	return nil
}

// close 写进最后一帧、回填时长、关文件。返回这个文件录了多少毫秒
func (m *mp4File) close() (int64, error) {
	var first error
	if p := m.pending; p != nil {
		// 最后一帧没有下一帧可量,按前一帧的时长算
		p.dur = m.lastDur
		if p.dur == 0 {
			p.dur = mediaTimescale / 30
		}
		m.frag = append(m.frag, *p)
		m.pending = nil
	}
	if err := m.flush(); err != nil {
		first = err
	}
	ms := m.endTicks * movieTimescale / mediaTimescale
	for _, d := range m.durs {
		v := uint64(m.endTicks)
		if d.ms {
			v = uint64(ms)
		}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], v)
		if _, err := m.f.WriteAt(b[:], d.off); err != nil && first == nil {
			first = err
		}
	}
	if err := m.f.Close(); err != nil && first == nil {
		first = err
	}
	return ms, first
}

// ---- 盒子 ----

// boxWriter 拼 MP4 盒子:先占 4 字节长度,内容写完再回填
type boxWriter struct {
	b    []byte
	durs []durField
}

func (w *boxWriter) box(typ string, body func()) {
	start := len(w.b)
	w.b = append(w.b, 0, 0, 0, 0)
	w.b = append(w.b, typ...)
	body()
	binary.BigEndian.PutUint32(w.b[start:], uint32(len(w.b)-start))
}

// fullBox 带版本号和标志位的盒子
func (w *boxWriter) fullBox(typ string, version byte, flags uint32, body func()) {
	w.box(typ, func() {
		w.u32(uint32(version)<<24 | flags)
		body()
	})
}

func (w *boxWriter) u8(v byte)      { w.b = append(w.b, v) }
func (w *boxWriter) u16(v uint16)   { w.b = binary.BigEndian.AppendUint16(w.b, v) }
func (w *boxWriter) u32(v uint32)   { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *boxWriter) u64(v uint64)   { w.b = binary.BigEndian.AppendUint64(w.b, v) }
func (w *boxWriter) zeros(n int)    { w.b = append(w.b, make([]byte, n)...) }
func (w *boxWriter) str(s string)   { w.b = append(w.b, s...) }
func (w *boxWriter) bytes(b []byte) { w.b = append(w.b, b...) }

// dur 占一个 8 字节的时长字段,记下位置等收尾时回填
func (w *boxWriter) dur(ms bool) {
	w.durs = append(w.durs, durField{off: int64(len(w.b)), ms: ms})
	w.u64(0)
}

// matrix 单位变换矩阵:不旋转、不缩放
func (w *boxWriter) matrix() {
	for _, v := range []uint32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000} {
		w.u32(v)
	}
}

// initSegment 文件头:ftyp + moov。moov 里一个视频轨,没有帧 —— 帧都在后面的分段里
func initSegment(width, height int, sps, pps []byte) *boxWriter {
	w := &boxWriter{}
	w.box("ftyp", func() {
		w.str("isom")
		w.u32(0x200)
		// iso5 表示分段里的数据偏移从 moof 算起(tfhd 的 default-base-is-moof)
		w.str("isomiso5iso6avc1mp41")
	})
	w.box("moov", func() {
		w.fullBox("mvhd", 1, 0, func() {
			w.u64(0) // 创建时间
			w.u64(0) // 修改时间
			w.u32(movieTimescale)
			w.dur(true)
			w.u32(0x10000) // 播放速率 1.0
			w.u16(0x100)   // 音量 1.0
			w.zeros(10)
			w.matrix()
			w.zeros(24)
			w.u32(2) // 下一个轨道号
		})
		w.box("trak", func() {
			w.fullBox("tkhd", 1, 3, func() { // 3 = 启用、参与播放
				w.u64(0)
				w.u64(0)
				w.u32(1) // 轨道号
				w.u32(0)
				w.dur(true)
				w.zeros(8)
				w.u16(0) // 层
				w.u16(0) // 备选组
				w.u16(0) // 音量:视频轨是 0
				w.u16(0)
				w.matrix()
				w.u32(uint32(width) << 16)
				w.u32(uint32(height) << 16)
			})
			w.box("mdia", func() {
				w.fullBox("mdhd", 1, 0, func() {
					w.u64(0)
					w.u64(0)
					w.u32(mediaTimescale)
					w.dur(false)
					w.u16(0x55c4) // 语言:und
					w.u16(0)
				})
				w.fullBox("hdlr", 0, 0, func() {
					w.u32(0)
					w.str("vide")
					w.zeros(12)
					w.str("VideoHandler\x00")
				})
				w.box("minf", func() {
					w.fullBox("vmhd", 0, 1, func() { w.zeros(8) })
					w.box("dinf", func() {
						w.fullBox("dref", 0, 0, func() {
							w.u32(1)
							w.fullBox("url ", 0, 1, func() {}) // 1 = 数据就在本文件里
						})
					})
					w.box("stbl", func() {
						w.fullBox("stsd", 0, 0, func() {
							w.u32(1)
							w.box("avc1", func() {
								w.zeros(6)
								w.u16(1) // 数据引用号
								w.zeros(16)
								w.u16(uint16(width))
								w.u16(uint16(height))
								w.u32(0x480000) // 72 dpi
								w.u32(0x480000)
								w.u32(0)
								w.u16(1) // 每个样本一帧
								w.zeros(32)
								w.u16(0x18)   // 色深 24 位
								w.u16(0xffff) // pre_defined = -1
								w.box("avcC", func() { w.avcConfig(sps, pps) })
							})
						})
						// 帧表都是空的:帧在分段里
						w.fullBox("stts", 0, 0, func() { w.u32(0) })
						w.fullBox("stsc", 0, 0, func() { w.u32(0) })
						w.fullBox("stsz", 0, 0, func() { w.u32(0); w.u32(0) })
						w.fullBox("stco", 0, 0, func() { w.u32(0) })
					})
				})
			})
		})
		w.box("mvex", func() {
			w.fullBox("mehd", 1, 0, func() { w.dur(true) })
			w.fullBox("trex", 0, 0, func() {
				w.u32(1) // 轨道号
				w.u32(1) // 默认用第一个样本描述
				w.u32(0)
				w.u32(0)
				w.u32(0)
			})
		})
	})
	return w
}

// avcConfig H.264 的解码配置:档次、级别,加上 SPS、PPS 原文
func (w *boxWriter) avcConfig(sps, pps []byte) {
	w.u8(1)
	w.u8(sps[1]) // 档次
	w.u8(sps[2]) // 兼容性标志
	w.u8(sps[3]) // 级别
	w.u8(0xff)   // NAL 长度用 4 字节
	w.u8(0xe1)   // 1 个 SPS
	w.u16(uint16(len(sps)))
	w.bytes(sps)
	w.u8(1) // 1 个 PPS
	w.u16(uint16(len(pps)))
	w.bytes(pps)
	switch sps[1] {
	case 100, 110, 122, 144:
		// High 一系的档次要多带色度格式和位深。手机硬件编码器出的都是 8 位 4:2:0
		w.u8(0xfc | 1)
		w.u8(0xf8)
		w.u8(0xf8)
		w.u8(0)
	}
}

// fragment 一段:moof 说明这几帧多长、多大、是不是关键帧,mdat 是帧数据本身
func fragment(seq uint32, base int64, samples []sample) []byte {
	w := &boxWriter{}
	dataOffsetAt := 0
	w.box("moof", func() {
		w.fullBox("mfhd", 0, 0, func() { w.u32(seq) })
		w.box("traf", func() {
			w.fullBox("tfhd", 0, 0x20000, func() { w.u32(1) }) // 数据偏移从 moof 开头算
			w.fullBox("tfdt", 1, 0, func() { w.u64(uint64(base)) })
			// 0x701 = 带数据偏移,每帧带时长、大小、标志
			w.fullBox("trun", 0, 0x701, func() {
				w.u32(uint32(len(samples)))
				dataOffsetAt = len(w.b)
				w.u32(0)
				for _, s := range samples {
					w.u32(s.dur)
					w.u32(uint32(len(s.data)))
					if s.key {
						w.u32(0x2000000) // 不依赖别的帧
					} else {
						w.u32(0x1010000) // 依赖别的帧,不能从这里开始播
					}
				}
			})
		})
	})
	// 帧数据紧跟在 mdat 的 8 字节头后面
	binary.BigEndian.PutUint32(w.b[dataOffsetAt:], uint32(len(w.b)+8))
	total := 0
	for _, s := range samples {
		total += len(s.data)
	}
	w.u32(uint32(8 + total))
	w.str("mdat")
	for _, s := range samples {
		w.bytes(s.data)
	}
	return w.b
}

// ---- H.264 字节流 ----

// splitNALs 把 Annex B 字节流(每个 NAL 前面是 00 00 01 或 00 00 00 01)切开
func splitNALs(b []byte) [][]byte {
	var out [][]byte
	start := -1
	for i := 0; i+3 <= len(b); {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			if start >= 0 {
				out = append(out, trimZeros(b[start:i]))
			}
			i += 3
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// trimZeros 去掉末尾的 0:那是下一个四字节起始码的头一个字节
func trimZeros(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}

// annexBToAVCC 一帧换成 MP4 里的写法:起始码换成 4 字节长度。
// SPS、PPS 已经在文件头里了,帧里要是也带着就去掉;分隔符(AUD)也不要
func annexBToAVCC(b []byte) []byte {
	var out []byte
	for _, n := range splitNALs(b) {
		if len(n) == 0 {
			continue
		}
		switch n[0] & 0x1f {
		case 7, 8, 9:
			continue
		}
		out = binary.BigEndian.AppendUint32(out, uint32(len(n)))
		out = append(out, n...)
	}
	return out
}

// parameterSets 从配置包里找出 SPS 和 PPS(各取第一个)
func parameterSets(b []byte) (sps, pps []byte) {
	for _, n := range splitNALs(b) {
		if len(n) == 0 {
			continue
		}
		switch n[0] & 0x1f {
		case 7:
			if sps == nil && len(n) >= 4 {
				sps = append([]byte(nil), n...)
			}
		case 8:
			if pps == nil {
				pps = append([]byte(nil), n...)
			}
		}
	}
	return sps, pps
}
