//go:build linux && cgo

package system

import "golang.design/x/hotkey"

func hotkeyAltModifier() hotkey.Modifier {
	return hotkey.Mod1
}

func isHotkeyAltModifier(mod hotkey.Modifier) bool {
	return mod == hotkey.Mod1
}
