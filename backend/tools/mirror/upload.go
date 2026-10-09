package mirror

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// iOS 投屏的截图、录屏在界面那头做:画面是界面里的 VNC 客户端解出来的,原样的像素就在它手里。
// 这里只管落盘 —— 文件名、不覆盖同名文件的规矩和安卓的一样

// SaveShot 存一张界面截下来的 PNG。label 是机型,进文件名
func (s *Service) SaveShot(dir, label string, data []byte) (*Shot, error) {
	if err := checkDir(dir); err != nil {
		return nil, err
	}
	if len(data) > maxScreenshot {
		return nil, errors.New("截图太大了")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("截下来的图打不开: %w", err)
	}
	path, err := writeNew(dir, captureName(label, "截图", time.Now()), ".png", data)
	if err != nil {
		return nil, err
	}
	return &Shot{Path: path, Width: cfg.Width, Height: cfg.Height, Bytes: len(data)}, nil
}

// upload 界面录着的屏,一段一段传过来往文件里追加
type upload struct {
	mu    sync.Mutex
	f     *os.File
	path  string
	bytes int64
	err   error
}

// uploads 正在录的几路。录屏 id 和投屏 id 不是一回事:投屏断了重连,录像还接着往同一个文件里写
type uploads struct {
	mu  sync.Mutex
	m   map[string]*upload
	seq int
}

// BeginUpload 开一个新的录屏文件。ext 是 .mp4 或 .webm,看界面那头录得出哪种
func (s *Service) BeginUpload(dir, label, ext string) (id, path string, err error) {
	if ext != ".mp4" && ext != ".webm" {
		return "", "", fmt.Errorf("不认识的录像格式 %q", ext)
	}
	if err := checkDir(dir); err != nil {
		return "", "", err
	}
	path = freeBase(filepath.Join(dir, captureName(label, "录屏", time.Now())), ext) + ext
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", "", fmt.Errorf("建不了录像文件: %w", err)
	}
	u := &upload{f: f, path: path}
	s.up.mu.Lock()
	if s.up.m == nil {
		s.up.m = map[string]*upload{}
	}
	s.up.seq++
	id = fmt.Sprintf("rec-%d", s.up.seq)
	s.up.m[id] = u
	s.up.mu.Unlock()
	return id, path, nil
}

// AppendUpload 往录像文件后面接一段。写盘出过错之后不再写,结束时一并报
func (s *Service) AppendUpload(id string, chunk []byte) error {
	u := s.upload(id)
	if u == nil {
		return errors.New("录屏已经结束了")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err != nil {
		return u.err
	}
	n, err := u.f.Write(chunk)
	u.bytes += int64(n)
	if err != nil {
		u.err = fmt.Errorf("录像写文件出错: %w", err)
	}
	return u.err
}

// EndUpload 录完了:关文件、补好时长。ms 是界面那头量的录了多久
func (s *Service) EndUpload(id string, ms int64) (*Recording, error) {
	s.up.mu.Lock()
	u := s.up.m[id]
	delete(s.up.m, id)
	s.up.mu.Unlock()
	if u == nil {
		return nil, errors.New("没在录屏")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.f.Close(); err != nil && u.err == nil {
		u.err = fmt.Errorf("录像写文件出错: %w", err)
	}
	if u.bytes == 0 {
		// 一段都没传过来:留个空文件只会让人以为录到了东西
		_ = os.Remove(u.path)
		return &Recording{Error: "没录到画面"}, nil
	}
	res := &Recording{Files: []string{u.path}, DurationMs: ms, Bytes: u.bytes}
	if u.err == nil {
		u.err = finishRecording(u.path)
	}
	if u.err != nil {
		res.Error = u.err.Error()
	}
	return res, nil
}

func (s *Service) upload(id string) *upload {
	s.up.mu.Lock()
	defer s.up.mu.Unlock()
	return s.up.m[id]
}

// closeUploads 应用退出时还在录的,把已经写下的收好
func (s *Service) closeUploads() {
	s.up.mu.Lock()
	ids := make([]string, 0, len(s.up.m))
	for id := range s.up.m {
		ids = append(ids, id)
	}
	s.up.mu.Unlock()
	for _, id := range ids {
		_, _ = s.EndUpload(id, 0)
	}
}
