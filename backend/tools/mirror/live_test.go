package mirror

import (
	"encoding/binary"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// 接真手机走一遍:推送、启动、收画面,然后停掉。只看不点 —— 不往手机发任何触摸和按键。
// 默认跳过;插着手机时这样跑(多台时用 MIRROR_SERIAL 指定):
//
//	MIRROR_LIVE=1 go test ./backend/tools/mirror -run TestLiveMirror -v
func TestLiveMirror(t *testing.T) {
	if os.Getenv("MIRROR_LIVE") != "1" {
		t.Skip("要接真手机;设 MIRROR_LIVE=1 才跑")
	}
	svc := New()
	defer svc.CloseAll()

	begin := time.Now()
	info, err := svc.Start(StartRequest{
		Serial:  os.Getenv("MIRROR_SERIAL"),
		Options: Options{MaxSize: 1024, BitRate: 4_000_000, MaxFps: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("设备 %s,启动用时 %v", info.DeviceName, time.Since(begin).Round(time.Millisecond))

	ws, _, err := websocket.DefaultDialer.Dial(info.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	var (
		width, height uint32
		codec         string
		frames, bytes int
		firstKey      time.Duration
	)
	attached := time.Now()
	deadline := attached.Add(15 * time.Second)
	for frames < 30 {
		_ = ws.SetReadDeadline(deadline)
		kind, p, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("收了 %d 帧之后断了: %v", frames, err)
		}
		if kind == websocket.TextMessage {
			t.Logf("文字消息: %s", p)
			continue
		}
		switch {
		case p[0]&0x80 != 0:
			width, height = binary.BigEndian.Uint32(p[4:]), binary.BigEndian.Uint32(p[8:])
		case p[0]&0x40 != 0:
			codec = codecFromConfig(p[headerSize:])
		default:
			if p[0]&0x20 != 0 && firstKey == 0 {
				firstKey = time.Since(attached)
			}
			frames++
			bytes += len(p) - headerSize
		}
	}
	if width == 0 || codec == "" || firstKey == 0 {
		t.Fatalf("少东西: 尺寸 %dx%d,编码 %q,第一个关键帧 %v", width, height, codec, firstKey)
	}
	t.Logf("画面 %dx%d,%s;接上后 %v 收到第一个关键帧;%d 帧共 %d KB",
		width, height, codec, firstKey.Round(time.Millisecond), frames, bytes/1024)

	svc.Stop(info.ID)
	if svc.get(info.ID) != nil {
		t.Fatal("停掉之后会话还在")
	}
}

// 接真手机录几秒屏、截一张图。同样只看不点;开着「保持亮屏」,顺带验证手机端认这个参数。
// 文件默认存在临时目录、跑完就删;要留下来拿播放器看,设 MIRROR_OUT 指一个文件夹:
//
//	MIRROR_LIVE=1 MIRROR_OUT=<文件夹> go test ./backend/tools/mirror -run TestLiveCapture -v
func TestLiveCapture(t *testing.T) {
	if os.Getenv("MIRROR_LIVE") != "1" {
		t.Skip("要接真手机;设 MIRROR_LIVE=1 才跑")
	}
	dir := os.Getenv("MIRROR_OUT")
	if dir == "" {
		dir = t.TempDir()
	}
	svc := New()
	defer svc.CloseAll()
	info, err := svc.Start(StartRequest{
		Serial:  os.Getenv("MIRROR_SERIAL"),
		Options: Options{MaxSize: 1920, BitRate: 8_000_000, MaxFps: 60, KeepAwake: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("设备 %s,安卓 API %d", info.DeviceName, info.SDK)
	ws, _, err := websocket.DefaultDialer.Dial(info.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	// 界面那头得一直有人收,不然转发卡住、录像也跟着卡
	go func() {
		for {
			kind, p, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage {
				t.Logf("文字消息: %s", p)
			}
		}
	}()

	path, err := svc.StartRecording(info.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * time.Second)
	rec, err := svc.StopRecording(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Files) != 1 || rec.Files[0] != path || rec.DurationMs < 3000 || rec.Error != "" {
		t.Fatalf("录屏结果不对: %+v", rec)
	}
	t.Logf("录屏 %s:%d 毫秒,%d KB", path, rec.DurationMs, rec.Bytes/1024)

	shot, err := svc.Screenshot(info.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("截图 %s:%dx%d,%d KB", shot.Path, shot.Width, shot.Height, shot.Bytes/1024)
	svc.Stop(info.ID)
}

// codecFromConfig 从配置包里的 SPS 拼出 WebCodecs 要的编码串,和界面那头的算法一致
func codecFromConfig(b []byte) string {
	for i := 0; i+4 < len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 && b[i+3]&0x1f == 7 && i+6 < len(b) {
			return fmt.Sprintf("avc1.%02x%02x%02x", b[i+4], b[i+5], b[i+6])
		}
	}
	return ""
}
