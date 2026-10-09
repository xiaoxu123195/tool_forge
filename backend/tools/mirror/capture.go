package mirror

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 截图和录屏。两样都存到用户选的文件夹,文件名带上机型和时间,不弹保存框 ——
// 操作手机的时候往往要连着截好几张,每张都问一遍存哪儿就没法用了

// ---- 截图 ----

// Shot 一张截图
type Shot struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  int    `json:"bytes"`
}

// maxScreenshot 一张截图最大多少字节。4K 屏的无损 PNG 也就几十 MB
const maxScreenshot = 128 << 20

// Screenshot 让手机自己截一张原图存进 dir。
//
// 不从投屏画面上截:那是压缩过的视频,还可能缩小过,字边上会有毛刺;
// 手机自己截的是屏幕上原原本本的像素
func (s *Service) Screenshot(id, dir string) (*Shot, error) {
	sess, err := s.live(id)
	if err != nil {
		return nil, err
	}
	if err := checkDir(dir); err != nil {
		return nil, err
	}
	data, err := screencap(sess.serial)
	if err != nil {
		return nil, err
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("手机给的截图打不开: %w", err)
	}
	path, err := writeNew(dir, captureName(sess.label(), "截图", time.Now()), ".png", data)
	if err != nil {
		return nil, err
	}
	return &Shot{Path: path, Width: cfg.Width, Height: cfg.Height, Bytes: len(data)}, nil
}

// screencap 在手机上跑 screencap,拿 PNG 原样的字节。
// 走 exec 而不是 shell:shell 在老系统上会经过终端,把图片里的 \n 改成 \r\n
func screencap(serial string) ([]byte, error) {
	c, err := openService(serial, "exec:screencap -p")
	if err != nil {
		return nil, fmt.Errorf("截图失败: %w", err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
	raw, err := io.ReadAll(io.LimitReader(c, maxScreenshot))
	if err != nil {
		return nil, fmt.Errorf("截图失败: %w", err)
	}
	return extractPNG(raw)
}

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// extractPNG 从 screencap 的输出里取出 PNG。
// 有几块屏的手机(折叠屏之类)会先打一行「没指定屏幕」的警告,和图片混在同一条输出里
func extractPNG(raw []byte) ([]byte, error) {
	i := bytes.Index(raw, pngMagic)
	if i < 0 {
		msg := strings.TrimSpace(string(raw))
		msg = msg[:utf8Prefix(msg, 200)]
		if msg == "" {
			msg = "手机没有给出图片"
		}
		return nil, fmt.Errorf("截图失败:%s", msg)
	}
	b := raw[i:]
	// 图片以 IEND 块结束(后面 4 字节是校验),再往后的不是图片
	if j := bytes.LastIndex(b, []byte("IEND")); j >= 0 && j+8 <= len(b) {
		b = b[:j+8]
	}
	return b, nil
}

// ---- 录屏 ----

// Recording 一次录屏的结果
type Recording struct {
	// Files 存成的文件。手机转过屏会有好几段:一个 MP4 只能有一种画面尺寸
	Files      []string `json:"files"`
	DurationMs int64    `json:"durationMs"`
	Bytes      int64    `json:"bytes"`
	// Error 中途出了错(写盘失败之类)。出错前录下来的照样能看
	Error string `json:"error,omitempty"`
}

// StartRecording 开始录屏,存进 dir。返回第一段文件的路径 —— 下一个关键帧到了它才真的建出来
func (s *Service) StartRecording(id, dir string) (string, error) {
	sess, err := s.live(id)
	if err != nil {
		return "", err
	}
	if err := checkDir(dir); err != nil {
		return "", err
	}
	base := freeBase(filepath.Join(dir, captureName(sess.label(), "录屏", time.Now())), ".mp4")
	sess.recMu.Lock()
	if sess.rec != nil {
		sess.recMu.Unlock()
		return "", errors.New("已经在录了")
	}
	sess.rec = newRecorder(base)
	sess.recMu.Unlock()
	// 录像得从关键帧开始:让手机端马上重起一段编码,不用干等下一个关键帧(最长要 10 秒)
	if err := sess.sendControl([]byte{msgResetVideo}); err != nil {
		sess.takeRecording()
		return "", fmt.Errorf("录屏没开起来: %w", err)
	}
	return base + ".mp4", nil
}

// StopRecording 停止录屏,把最后一段写完
func (s *Service) StopRecording(id string) (*Recording, error) {
	sess := s.get(id)
	if sess == nil {
		return nil, errors.New("投屏已经断开了,断开前录下来的已经存好")
	}
	res, ok := sess.takeRecording()
	if !ok {
		return nil, errors.New("没在录屏")
	}
	return &res, nil
}

func recordedNotice(res Recording, errText string) notice {
	if errText == "" {
		errText = res.Error
	}
	return notice{Type: "recorded", Text: errText, Files: res.Files, Ms: res.DurationMs}
}

// recorder 一次录屏。手机发来的包原样往 MP4 里放;转屏后尺寸变了,另起一个文件接着录
type recorder struct {
	// base 第一段的路径,不带扩展名。后面的段加 _2、_3
	base  string
	files []string
	ms    int64
	bytes int64
	err   error

	// 下一段要用的编码参数:会话包给尺寸,配置包给 SPS、PPS
	width, height int
	sps, pps      []byte

	out *mp4File
	// t0 这一段第一帧在录像时间轴上的位置
	t0 int64

	// 时间轴(微秒)。手机每重起一段编码,时间戳可能从头算;这里拼成一条连续往前走的
	begun    bool
	rebase   bool
	offset   int64
	lastOut  int64
	lastWall time.Time
	now      func() time.Time
}

func newRecorder(base string) *recorder {
	return &recorder{base: base, now: time.Now}
}

// feed 收一个包(和发给界面的是同一个)
func (r *recorder) feed(pkt []byte) error {
	if len(pkt) < headerSize {
		return nil
	}
	switch {
	case pkt[0]&0x80 != 0:
		w, h := int(binary.BigEndian.Uint32(pkt[4:])), int(binary.BigEndian.Uint32(pkt[8:]))
		if r.out != nil && (w != r.width || h != r.height) {
			if err := r.endPart(); err != nil {
				return err
			}
		}
		r.width, r.height = w, h
		r.rebase = true
	case pkt[0]&0x40 != 0:
		sps, pps := parameterSets(pkt[headerSize:])
		if sps == nil || pps == nil {
			return nil
		}
		if r.out != nil && (!bytes.Equal(sps, r.sps) || !bytes.Equal(pps, r.pps)) {
			if err := r.endPart(); err != nil {
				return err
			}
		}
		r.sps, r.pps = sps, pps
	default:
		key := pkt[0]&0x20 != 0
		t := r.timeline(int64(binary.BigEndian.Uint64(pkt) & (1<<61 - 1)))
		if r.out == nil {
			// 新的一段只能从关键帧开始:前面的帧依赖着上一段
			if !key || r.sps == nil || r.width <= 0 || r.height <= 0 {
				return nil
			}
			if err := r.startPart(); err != nil {
				return err
			}
			r.t0 = t
		}
		data := annexBToAVCC(pkt[headerSize:])
		if len(data) == 0 {
			return nil
		}
		return r.out.add(sample{ticks: (t - r.t0) * mediaTimescale / 1_000_000, key: key, data: data})
	}
	return nil
}

// timeline 手机的时间戳 → 录像里的时间(微秒),保证一直往前走
func (r *recorder) timeline(pts int64) int64 {
	now := r.now()
	switch {
	case !r.begun:
		r.offset = -pts
	case r.rebase:
		// 中间重起过一段编码(界面重连、解码器重来):接在上一帧后面,间隔按真实过去的时间算
		gap := now.Sub(r.lastWall).Microseconds()
		r.offset = r.lastOut + max(gap, 1000) - pts
	}
	r.rebase = false
	t := pts + r.offset
	if r.begun && t <= r.lastOut {
		t = r.lastOut + 1
	}
	r.begun = true
	r.lastOut, r.lastWall = t, now
	return t
}

func (r *recorder) startPart() error {
	path := r.base + ".mp4"
	if n := len(r.files); n > 0 {
		path = freeBase(fmt.Sprintf("%s_%d", r.base, n+1), ".mp4") + ".mp4"
	}
	m, err := createMP4(path, r.width, r.height, r.sps, r.pps)
	if err != nil {
		return err
	}
	r.out = m
	r.files = append(r.files, path)
	return nil
}

func (r *recorder) endPart() error {
	if r.out == nil {
		return nil
	}
	ms, err := r.out.close()
	r.ms += ms
	r.bytes += r.out.size
	r.out = nil
	return err
}

// finish 收尾。录屏一个关键帧都没等到就停了的话,Files 是空的
func (r *recorder) finish() Recording {
	if err := r.endPart(); err != nil && r.err == nil {
		r.err = err
	}
	res := Recording{Files: r.files, DurationMs: r.ms, Bytes: r.bytes}
	if r.err != nil {
		res.Error = r.err.Error()
	}
	return res
}

// ---- 文件名 ----

// label 文件名里用的机型。手机没报型号就用序列号
func (s *session) label() string {
	if s.deviceName != "" {
		return s.deviceName
	}
	return s.serial
}

// captureName 机型_截图_20261009_153012 这样的名字,不带扩展名
func captureName(label, kind string, t time.Time) string {
	return safeName(label) + "_" + kind + "_" + t.Format("20060102_150405")
}

// safeName 去掉 Windows 文件名里不能有的字符
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.Trim(s, " .")
	if s == "" {
		return "手机"
	}
	return s
}

// checkDir 保存的文件夹得还在
func checkDir(dir string) error {
	if dir == "" {
		return errors.New("还没选保存的文件夹")
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return fmt.Errorf("保存的文件夹不在了:%s", dir)
	}
	return nil
}

// freeBase 同一秒里截了好几张:第二张起加 _2、_3。返回不带扩展名的路径
func freeBase(base, ext string) string {
	for i := 1; i < 1000; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s_%d", base, i)
		}
		if _, err := os.Stat(cand + ext); errors.Is(err, os.ErrNotExist) {
			return cand
		}
	}
	return base
}

// writeNew 写一个新文件,已经有同名的绝不覆盖
func writeNew(dir, name, ext string, data []byte) (string, error) {
	path := freeBase(filepath.Join(dir, name), ext) + ext
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("保存失败: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("保存失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("保存失败: %w", err)
	}
	return path, nil
}
