// Package devicefs 直接浏览连着的手机上的文件系统。
//
// 为什么不是"浏览导出结果":先导出再看,意味着你得先猜对要导哪个目录。
// 而取证现场恰恰是反过来的 —— 先翻,翻到有价值的再取。
//
// 两个平台的路子完全不同,但上层要的东西一样,所以用 transport 抽一层:
//
//	iOS      usbmuxd 直连设备的 22 端口 → SSH → SFTP
//	Android  adb shell → su → 设备自带的 toybox
//
// 落到具体实现上的差别比看起来大:iOS 那边 SFTP 给的是带类型的结构化响应;
// Android 那边只有一个 shell,所有信息都得从命令输出里解析出来。
package devicefs

import (
	"errors"
	"fmt"
	"sync"
)

// transport 一台设备上的文件访问方式。
//
// 抽这一层不是为了好看:两个平台唯一的共同点就是这几个动作,
// 别的全不一样。把差异按在这条线以下,上面的目录树、预览、搜索才能只写一份。
type transport interface {
	// list 列一个目录
	list(dir string) (*Listing, error)
	// stat 单个条目的信息
	stat(p string) (*Entry, error)
	// pull 把设备上的文件拉到本地;limit > 0 表示只要开头那么多字节。
	// 返回实际字节数和是否被截断
	pull(remote, local string, limit int64) (int64, bool, error)
	// search 按文件名递归查找
	search(root, pattern string, limit int) (*SearchResult, error)
	// exists 判断一个路径在不在(MMKV 认 .crc 要用)
	exists(p string) bool
	// startPath 打开时落在哪个目录
	startPath() string
	// previewLimit 预览最多从设备上拉多少字节。
	// 两个平台差一个数量级:SFTP 能跑满 USB,而 Android 那边预览走的是
	// base64 文本通道(约 1.4 MB/s),同样的上限会让人对着转圈等好几秒
	previewLimit() int64
	close() error
}

// Session 一个连上的设备
type Session struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	// DeviceID 设备标识:iOS 是 UDID,Android 是序列号
	DeviceID string `json:"deviceId"`
	// Addr 连到哪儿了,排查问题时有用。
	// iOS 是 usb:<UDID 缩写>:<端口>,Android 是 adb 序列号
	Addr string `json:"addr"`
	// StartPath 建议的起始目录
	StartPath string `json:"startPath"`
	// Rooted Android 上 su 能不能用。用不了的话只看得到 /sdcard,
	// 而有价值的数据全在 /data/data 下面 —— 这个必须让人一眼看到
	Rooted bool `json:"rooted"`
	// Model 设备型号,连了多台时用来确认连对了没有
	Model string `json:"model,omitempty"`

	t transport
}

// Manager 管着所有活着的会话。
//
// 前端拿到的是 session id,后续每次调用都带着它回来 —— 连接是有状态的
// (iOS 下面挂着一条 SSH/SFTP 连接,Android 下面记着序列号和 root 状态),
// 不可能每次调用重建一遍
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	seq      int
}

func NewManager() *Manager {
	return &Manager{sessions: map[string]*Session{}}
}

// ConnectOptions 连接参数
type ConnectOptions struct {
	// Platform ios | android
	Platform string `json:"platform"`
	// DeviceID iOS 的 UDID 或 Android 的序列号;空 = 第一台
	DeviceID string `json:"deviceId"`
	// User SSH 用户名,仅 iOS
	User string `json:"user"`
	// Password SSH 密码,仅 iOS
	Password string `json:"password"`
	// AdbPath adb 路径,仅 Android;空 = 走 PATH
	AdbPath string `json:"adbPath"`
	// RemotePort 设备上的 SSH 端口,仅 iOS,默认 22
	RemotePort int `json:"remotePort"`
}

// Connect 连上一台设备
func (m *Manager) Connect(opt ConnectOptions) (*Session, error) {
	var (
		t   transport
		s   *Session
		err error
	)
	switch opt.Platform {
	case "android":
		t, s, err = connectAndroid(opt)
	case "ios", "":
		t, s, err = connectIOS(opt)
	default:
		return nil, fmt.Errorf("不认识的平台 %q,可选 ios / android", opt.Platform)
	}
	if err != nil {
		return nil, err
	}

	s.t = t
	s.StartPath = t.startPath()
	m.mu.Lock()
	m.seq++
	s.ID = fmt.Sprintf("dev-%d", m.seq)
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s, nil
}

// get 取一个会话;找不到就明说,别让调用方对着 nil 猜
func (m *Manager) get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("会话 %q 已经不在了(可能已断开),重新连接一次", id)
	}
	return s, nil
}

// Disconnect 断开并清理
func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if !ok {
		return nil // 已经没了,重复点断开不该报错
	}
	return s.t.close()
}

// CloseAll 应用退出时调。
// iOS 那边下面挂着一条经 USB 通到设备的连接,不收的话它会一直占着设备的通道
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range all {
		_ = s.t.close()
	}
}

var errNoPassword = errors.New("需要 SSH 密码 —— 越狱设备默认是 alpine,改过就填改后的")
