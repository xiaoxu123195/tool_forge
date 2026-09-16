//go:build !windows

package ocr

import (
	"context"
	"errors"
)

func recognize(_ context.Context, _ string) (*Result, error) {
	return nil, errors.New("这个平台还不支持文字识别，目前只接了 Windows 自带的 OCR")
}
