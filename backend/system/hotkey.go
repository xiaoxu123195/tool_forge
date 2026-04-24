package system

import (
	"context"
	"log"
	goruntime "runtime"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.design/x/hotkey"
)

// HotkeySpec 描述一组要注册的全局热键
type HotkeySpec struct {
	Mods  []hotkey.Modifier
	Key   hotkey.Key
	Event string // 触发后通过 wails emit 的事件名;前端监听做导航等
	Label string // 用于日志
}

// RegisterGlobalHotkeys 注册一批全局热键。
// macOS 上注册全局热键需要 hotkey 包独占主线程,与 Wails 冲突,直接跳过。
// Windows/Linux 注册失败时只记录日志,不阻断启动。
func RegisterGlobalHotkeys(ctx context.Context, specs []HotkeySpec) {
	if goruntime.GOOS == "darwin" {
		log.Printf("[hotkey] macOS 暂不支持全局热键,已跳过")
		return
	}
	for _, spec := range specs {
		go registerOne(ctx, spec)
	}
}

func registerOne(ctx context.Context, spec HotkeySpec) {
	hk := hotkey.New(spec.Mods, spec.Key)
	if err := hk.Register(); err != nil {
		log.Printf("[hotkey] 注册 %s 失败: %v", spec.Label, err)
		return
	}
	log.Printf("[hotkey] 已注册 %s", spec.Label)
	defer hk.Unregister()
	for {
		select {
		case <-ctx.Done():
			return
		case <-hk.Keydown():
			wailsruntime.WindowShow(ctx)
			wailsruntime.WindowUnminimise(ctx)
			wailsruntime.EventsEmit(ctx, spec.Event)
		}
	}
}
