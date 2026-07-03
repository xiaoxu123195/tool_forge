package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.design/x/hotkey"
)

// Action 描述一个可绑定的动作
type Action struct {
	ID          string // "clipboard.open"
	Label       string // 用户可见的中文名
	Event       string // 触发后通过 wails emit 的事件名
	DefaultSpec string // 默认按键组合,例如 "Ctrl+Shift+V"
}

// HotkeyInfo 是返给前端的状态
type HotkeyInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	DefaultSpec string `json:"defaultSpec"`
	CurrentSpec string `json:"currentSpec"`
	Active      bool   `json:"active"` // 是否实际注册成功
	Error       string `json:"error,omitempty"`
}

// Manager 管理一组可动态重绑定的全局热键
type Manager struct {
	ctx        context.Context
	configPath string
	actions    []Action
	mu         sync.Mutex
	bindings   map[string]string             // actionID → spec
	cancels    map[string]context.CancelFunc // actionID → 取消正在监听的 goroutine
	errors     map[string]string             // actionID → 最近一次注册错误
}

// NewManager 创建一个 Manager。configPath 决定持久化文件位置。
// 启动时会读取已存在的 bindings,缺失项 fall back 到 DefaultSpec。
func NewManager(actions []Action, configPath string) *Manager {
	m := &Manager{
		configPath: configPath,
		actions:    actions,
		bindings:   make(map[string]string),
		cancels:    make(map[string]context.CancelFunc),
		errors:     make(map[string]string),
	}
	// 读持久化
	if data, err := os.ReadFile(configPath); err == nil {
		var saved map[string]string
		if json.Unmarshal(data, &saved) == nil {
			m.bindings = saved
		}
	}
	// 缺省项填默认
	for _, a := range actions {
		if _, ok := m.bindings[a.ID]; !ok {
			m.bindings[a.ID] = a.DefaultSpec
		}
	}
	return m
}

// Start 在 ctx 生效后开始注册所有 binding
func (m *Manager) Start(ctx context.Context) {
	if goruntime.GOOS == "darwin" {
		log.Printf("[hotkey] macOS 暂不支持全局热键,已跳过")
		return
	}
	m.ctx = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.actions {
		spec := m.bindings[a.ID]
		if spec == "" {
			continue
		}
		m.registerLocked(a, spec)
	}
}

// Stop 取消所有当前绑定
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.cancels {
		cancel()
		delete(m.cancels, id)
	}
}

// List 返回所有 action 的当前状态
func (m *Manager) List() []HotkeyInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]HotkeyInfo, 0, len(m.actions))
	for _, a := range m.actions {
		spec := m.bindings[a.ID]
		_, active := m.cancels[a.ID]
		info := HotkeyInfo{
			ID:          a.ID,
			Label:       a.Label,
			DefaultSpec: a.DefaultSpec,
			CurrentSpec: spec,
			Active:      active,
		}
		if e, ok := m.errors[a.ID]; ok {
			info.Error = e
		}
		out = append(out, info)
	}
	return out
}

// Set 重绑某个 action;空字符串或 "off" 表示取消绑定
func (m *Manager) Set(id, spec string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.findAction(id)
	if !ok {
		return fmt.Errorf("未知 action: %s", id)
	}
	// 先取消旧的
	if cancel, ok := m.cancels[id]; ok {
		cancel()
		delete(m.cancels, id)
	}
	delete(m.errors, id)
	spec = strings.TrimSpace(spec)
	m.bindings[id] = spec
	if spec != "" && !strings.EqualFold(spec, "off") {
		// 校验后再注册
		if _, _, err := parseSpec(spec); err != nil {
			m.errors[id] = err.Error()
			m.persistLocked()
			return err
		}
		// 与其他 action 冲突检测
		for otherID, otherSpec := range m.bindings {
			if otherID == id {
				continue
			}
			if normalizeSpec(otherSpec) == normalizeSpec(spec) {
				err := fmt.Errorf("与 %s 冲突", otherID)
				m.errors[id] = err.Error()
				m.persistLocked()
				return err
			}
		}
		m.registerLocked(a, spec)
	}
	m.persistLocked()
	return nil
}

// Reset 把某个 action 还原成默认
func (m *Manager) Reset(id string) error {
	a, ok := m.findAction(id)
	if !ok {
		return fmt.Errorf("未知 action: %s", id)
	}
	return m.Set(id, a.DefaultSpec)
}

func (m *Manager) findAction(id string) (Action, bool) {
	for _, a := range m.actions {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}

func (m *Manager) persistLocked() {
	if m.configPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o755); err != nil {
		log.Printf("[hotkey] mkdir config 失败: %v", err)
		return
	}
	data, _ := json.MarshalIndent(m.bindings, "", "  ")
	if err := os.WriteFile(m.configPath, data, 0o644); err != nil {
		log.Printf("[hotkey] 写 config 失败: %v", err)
	}
}

func (m *Manager) registerLocked(a Action, spec string) {
	if m.ctx == nil {
		return
	}
	mods, key, err := parseSpec(spec)
	if err != nil {
		m.errors[a.ID] = err.Error()
		return
	}
	hk := hotkey.New(mods, key)
	if err := hk.Register(); err != nil {
		m.errors[a.ID] = err.Error()
		log.Printf("[hotkey] 注册 %s (%s) 失败: %v", a.ID, spec, err)
		return
	}
	log.Printf("[hotkey] 已注册 %s = %s", a.ID, spec)
	subCtx, cancel := context.WithCancel(m.ctx)
	m.cancels[a.ID] = cancel
	go func() {
		defer hk.Unregister()
		for {
			select {
			case <-subCtx.Done():
				return
			case <-hk.Keydown():
				wailsruntime.WindowShow(m.ctx)
				wailsruntime.WindowUnminimise(m.ctx)
				wailsruntime.EventsEmit(m.ctx, a.Event)
			}
		}
	}()
}

// ===================== Spec 解析 =====================

// parseSpec 把 "Ctrl+Shift+V" 这样的字符串解析成 hotkey 库的修饰键 + 主键
func parseSpec(spec string) ([]hotkey.Modifier, hotkey.Key, error) {
	parts := strings.Split(spec, "+")
	if len(parts) == 0 {
		return nil, 0, errors.New("空")
	}
	var mods []hotkey.Modifier
	keyPart := strings.TrimSpace(parts[len(parts)-1])
	for _, p := range parts[:len(parts)-1] {
		mod, err := parseMod(strings.TrimSpace(p))
		if err != nil {
			return nil, 0, err
		}
		mods = append(mods, mod)
	}
	key, err := parseKey(keyPart)
	if err != nil {
		return nil, 0, err
	}
	if len(mods) == 0 {
		return nil, 0, errors.New("至少需要一个修饰键 (Ctrl / Shift / Alt)")
	}
	return mods, key, nil
}

func parseMod(s string) (hotkey.Modifier, error) {
	switch strings.ToLower(s) {
	case "ctrl", "control":
		return hotkey.ModCtrl, nil
	case "shift":
		return hotkey.ModShift, nil
	case "alt", "option":
		return hotkeyAltModifier(), nil
	}
	return 0, fmt.Errorf("未知修饰键: %s", s)
}

func parseKey(s string) (hotkey.Key, error) {
	if s == "" {
		return 0, errors.New("缺少主键")
	}
	upper := strings.ToUpper(s)
	// A-Z
	if len(upper) == 1 {
		c := upper[0]
		if c >= 'A' && c <= 'Z' {
			return letterKey(c), nil
		}
		if c >= '0' && c <= '9' {
			return digitKey(c), nil
		}
	}
	// F1-F12
	if strings.HasPrefix(upper, "F") && len(upper) <= 3 {
		var n int
		fmt.Sscanf(upper[1:], "%d", &n)
		if n >= 1 && n <= 12 {
			return funcKey(n), nil
		}
	}
	return 0, fmt.Errorf("不支持的主键: %s (只支持 A-Z / 0-9 / F1-F12)", s)
}

func letterKey(c byte) hotkey.Key {
	switch c {
	case 'A':
		return hotkey.KeyA
	case 'B':
		return hotkey.KeyB
	case 'C':
		return hotkey.KeyC
	case 'D':
		return hotkey.KeyD
	case 'E':
		return hotkey.KeyE
	case 'F':
		return hotkey.KeyF
	case 'G':
		return hotkey.KeyG
	case 'H':
		return hotkey.KeyH
	case 'I':
		return hotkey.KeyI
	case 'J':
		return hotkey.KeyJ
	case 'K':
		return hotkey.KeyK
	case 'L':
		return hotkey.KeyL
	case 'M':
		return hotkey.KeyM
	case 'N':
		return hotkey.KeyN
	case 'O':
		return hotkey.KeyO
	case 'P':
		return hotkey.KeyP
	case 'Q':
		return hotkey.KeyQ
	case 'R':
		return hotkey.KeyR
	case 'S':
		return hotkey.KeyS
	case 'T':
		return hotkey.KeyT
	case 'U':
		return hotkey.KeyU
	case 'V':
		return hotkey.KeyV
	case 'W':
		return hotkey.KeyW
	case 'X':
		return hotkey.KeyX
	case 'Y':
		return hotkey.KeyY
	case 'Z':
		return hotkey.KeyZ
	}
	return 0
}

func digitKey(c byte) hotkey.Key {
	switch c {
	case '0':
		return hotkey.Key0
	case '1':
		return hotkey.Key1
	case '2':
		return hotkey.Key2
	case '3':
		return hotkey.Key3
	case '4':
		return hotkey.Key4
	case '5':
		return hotkey.Key5
	case '6':
		return hotkey.Key6
	case '7':
		return hotkey.Key7
	case '8':
		return hotkey.Key8
	case '9':
		return hotkey.Key9
	}
	return 0
}

func funcKey(n int) hotkey.Key {
	switch n {
	case 1:
		return hotkey.KeyF1
	case 2:
		return hotkey.KeyF2
	case 3:
		return hotkey.KeyF3
	case 4:
		return hotkey.KeyF4
	case 5:
		return hotkey.KeyF5
	case 6:
		return hotkey.KeyF6
	case 7:
		return hotkey.KeyF7
	case 8:
		return hotkey.KeyF8
	case 9:
		return hotkey.KeyF9
	case 10:
		return hotkey.KeyF10
	case 11:
		return hotkey.KeyF11
	case 12:
		return hotkey.KeyF12
	}
	return 0
}

// normalizeSpec 把 "shift+ctrl+v" 标准化为 "Ctrl+Shift+V" 用于冲突比较
func normalizeSpec(spec string) string {
	mods, key, err := parseSpec(spec)
	if err != nil {
		return strings.ToLower(spec)
	}
	hasCtrl := false
	hasShift := false
	hasAlt := false
	for _, m := range mods {
		switch m {
		case hotkey.ModCtrl:
			hasCtrl = true
		case hotkey.ModShift:
			hasShift = true
		default:
			if isHotkeyAltModifier(m) {
				hasAlt = true
			}
		}
	}
	parts := []string{}
	if hasCtrl {
		parts = append(parts, "Ctrl")
	}
	if hasShift {
		parts = append(parts, "Shift")
	}
	if hasAlt {
		parts = append(parts, "Alt")
	}
	parts = append(parts, keyName(key))
	return strings.Join(parts, "+")
}

func keyName(k hotkey.Key) string {
	for c := byte('A'); c <= 'Z'; c++ {
		if letterKey(c) == k {
			return string(c)
		}
	}
	for c := byte('0'); c <= '9'; c++ {
		if digitKey(c) == k {
			return string(c)
		}
	}
	for n := 1; n <= 12; n++ {
		if funcKey(n) == k {
			return fmt.Sprintf("F%d", n)
		}
	}
	return "?"
}
