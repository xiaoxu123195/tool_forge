//go:build darwin && cgo

package system

import "golang.design/x/hotkey"

func hotkeyAltModifier() hotkey.Modifier {
	return hotkey.ModOption
}

func isHotkeyAltModifier(mod hotkey.Modifier) bool {
	return mod == hotkey.ModOption
}
