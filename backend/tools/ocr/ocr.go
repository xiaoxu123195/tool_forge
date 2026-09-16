// Package ocr 从图片里认字。
//
// 取证现场大量信息是以截图的形式到手的:手机屏幕上的账号、转账记录、聊天里的电话号码。
// 想把它们变成能搜、能粘的文字,以前得手打一遍。
//
// 不带任何模型:Windows 自带 OCR(Windows.Media.Ocr),识别质量不差、中英文都有,
// 而且零依赖 —— 塞一个几十 MB 的 tesseract 进包里不值得。别的平台先不接。
package ocr

import (
	"context"
	"strings"
	"unicode"
)

// Result 一次识别的结果
type Result struct {
	// Text 整段文字,按行拼接
	Text string `json:"text"`
	// Lines 一行一条
	Lines []string `json:"lines"`
	// Lang 用的是哪个语言包(如 zh-Hans-CN)
	Lang string `json:"lang"`
}

// Recognize 识别一张图片里的文字
func Recognize(ctx context.Context, imagePath string) (*Result, error) {
	return recognize(ctx, imagePath)
}

// assemble 把识别出来的行整理成结果:去掉空行、去掉中文字之间被塞进来的空格
func assemble(lang string, raw []string) *Result {
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		if t := tidyLine(l); t != "" {
			lines = append(lines, t)
		}
	}
	return &Result{Text: strings.Join(lines, "\n"), Lines: lines, Lang: lang}
}

// tidyLine 去掉中文里多出来的空格。
//
// 系统 OCR 是按"词"给结果的,中文没有词间空格这回事,它就把每个字当一个词,
// 拼回去是"你 好 世 界"。两个汉字之间的空格去掉,中文标点两边的也去掉(中文标点
// 从不带空格);汉字和英文数字之间的留着 —— "转账 500 元"里那个空格原图上可能就有
func tidyLine(s string) string {
	rs := []rune(strings.TrimSpace(s))
	var b strings.Builder
	for i, r := range rs {
		if r == ' ' && i > 0 && i+1 < len(rs) {
			prev, next := rs[i-1], rs[i+1]
			if (isHan(prev) && isHan(next)) || isCJKPunct(prev) || isCJKPunct(next) {
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isHan(r rune) bool { return unicode.Is(unicode.Han, r) }

// isCJKPunct 中文标点:CJK 符号区,加上全角区里非字母数字的那几段
func isCJKPunct(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F:
		return true
	case r >= 0xFF01 && r <= 0xFF0F, r >= 0xFF1A && r <= 0xFF20,
		r >= 0xFF3B && r <= 0xFF40, r >= 0xFF5B && r <= 0xFF65:
		return true
	}
	return false
}
