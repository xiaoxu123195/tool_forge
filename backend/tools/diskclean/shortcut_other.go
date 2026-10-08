//go:build !windows

package diskclean

// shortcutPlaces .lnk 只有 Windows 上有
func shortcutPlaces() []shortcutPlace { return nil }

func decodeANSI(b []byte) string { return string(b) }

func pathFromIDList([]byte) string { return "" }

func localFixed(string) bool { return true }
