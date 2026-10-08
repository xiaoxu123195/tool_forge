//go:build !windows

package system

import clipx "golang.design/x/clipboard"

// CopyImage 把一张 PNG 放进系统剪贴板
func CopyImage(pngData []byte) error {
	if err := clipx.Init(); err != nil {
		return err
	}
	clipx.Write(clipx.FmtImage, pngData)
	return nil
}
