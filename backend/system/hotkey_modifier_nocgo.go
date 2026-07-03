//go:build !windows && !cgo

package system

import "golang.design/x/hotkey"

func hotkeyAltModifier() hotkey.Modifier {
	return 0
}

func isHotkeyAltModifier(mod hotkey.Modifier) bool {
	return false
}
