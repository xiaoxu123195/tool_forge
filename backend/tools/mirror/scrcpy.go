package mirror

import (
	_ "embed"
	"fmt"
	"strings"
)

// 手机端程序:scrcpy 的服务端(Apache-2.0)。来源、校验值、怎么换版本见 bundled/README.md
//
//go:embed bundled/scrcpy-server-v5.0.1
var serverJar []byte

const (
	// serverVersion 启动时的第一个参数,必须和手机端程序的版本一字不差,对不上它直接退出
	serverVersion = "5.0.1"
	// serverSHA256 和官方发布页 SHA256SUMS.txt 里的一致。换版本时一起改,测试会核对
	serverSHA256 = "764eb6f79811d5211fe9df341120882ba9994c7a61b897d7bf3fb662e53bc536"
	// devicePath 推到手机上的位置:shell 用户在 /data/local/tmp 下能写。
	// 程序一启动,它的清理进程就把这个文件删掉
	devicePath = "/data/local/tmp/scrcpy-server.jar"
)

// Options 投屏参数
type Options struct {
	// MaxSize 画面长边最多多少像素,0 = 手机原始分辨率
	MaxSize int `json:"maxSize"`
	// BitRate 视频码率,bit/s
	BitRate int `json:"bitRate"`
	// MaxFps 最高帧率,0 = 不限
	MaxFps int `json:"maxFps"`
}

func (o Options) validate() error {
	if o.MaxSize != 0 && (o.MaxSize < 320 || o.MaxSize > 4096) {
		return fmt.Errorf("画面尺寸 %d 不在 320~4096 之间", o.MaxSize)
	}
	if o.BitRate < 500_000 || o.BitRate > 50_000_000 {
		return fmt.Errorf("码率 %d 不在 0.5~50 Mbps 之间", o.BitRate)
	}
	if o.MaxFps < 0 || o.MaxFps > 120 {
		return fmt.Errorf("帧率 %d 不在 0~120 之间", o.MaxFps)
	}
	return nil
}

// serverCommand 在手机上启动手机端程序的那行命令。
//
// 参数只拼校验过的整数,不会有引号、分号之类混进 shell。
// 刻意留着默认值的几项:
//   - 不改手机设置:show_touches、stay_awake 都不开 —— 取证现场动了设置就得写进记录
//   - cleanup 默认开:清理进程启动时删掉推上去的程序文件
//   - power_on 默认开:黑着屏投过来是一块黑
func serverCommand(scid uint32, o Options) string {
	args := []string{
		"CLASSPATH=" + devicePath,
		"app_process", "/", "com.genymobile.scrcpy.Server", serverVersion,
		fmt.Sprintf("scid=%08x", scid),
		"log_level=info",
		// 手机那头监听、电脑这头去连:不用在电脑上开端口等手机连回来
		"tunnel_forward=true",
		"audio=false",
		// 界面上的解码器只认得稳 H.264:H.265 在没有硬解的机器上解不了
		"video_codec=h264",
		fmt.Sprintf("max_size=%d", o.MaxSize),
		fmt.Sprintf("video_bit_rate=%d", o.BitRate),
		// 手机剪贴板一变就推过来 —— 这一轮用不上,开着还得有人去读
		"clipboard_autosync=false",
	}
	if o.MaxFps > 0 {
		args = append(args, fmt.Sprintf("max_fps=%d", o.MaxFps))
	}
	return strings.Join(args, " ")
}

// socketName 手机端程序监听的本地套接字名。scid 让同一台手机上能同时有几路,互不相干
func socketName(scid uint32) string {
	return fmt.Sprintf("scrcpy_%08x", scid)
}
