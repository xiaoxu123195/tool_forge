//go:build windows

package system

import "golang.design/x/hotkey"

func hotkeyAltModifier() hotkey.Modifier {
	return hotkey.ModAlt
}

func isHotkeyAltModifier(mod hotkey.Modifier) bool {
	return mod == hotkey.ModAlt
}
