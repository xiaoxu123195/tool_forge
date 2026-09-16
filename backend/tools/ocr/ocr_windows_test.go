//go:build windows

package ocr

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const sample = "HELLO WORLD 2026"

// 真的把 PowerShell + WinRT 那条路走一遍。
//
// 光测 tidyLine 说明不了脚本能跑:WinRT 类型加载、AsTask 反射、输出编码、JSON 形状,
// 哪一环错了都是"点了没反应"。画一张大字的图喂进去,认出来才算通。
// 这台机器没装 OCR 语言包的话跳过 —— 那是环境问题,不是代码问题
func TestRecognizeRealEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("要起 PowerShell,-short 时跳过")
	}
	// 路径里故意带空格、单引号和中文:脚本里路径是单引号包的,这几样最容易把它弄坏
	p := filepath.Join(t.TempDir(), "文字 'quote'.png")
	if err := os.WriteFile(p, renderText(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Recognize(context.Background(), p)
	if err != nil {
		if strings.Contains(err.Error(), "语言包") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if res.Lang == "" {
		t.Error("没报用的是哪个语言包")
	}
	if len(res.Lines) == 0 {
		t.Fatalf("一行都没认出来: %+v", res)
	}
	// 引擎偶尔把 O 认成 Ü、漏一个数字;这里验的是管道通不通,
	// 认出来的字符按顺序能对上一半以上就算通,全错或者空的才是管道坏了
	if r := subsequenceRatio(sample, res.Text); r < 0.5 {
		t.Errorf("认出来的和原文对不上(%.0f%%): %q", r*100, res.Text)
	} else {
		t.Logf("%s → %q (%.0f%%)", sample, res.Text, r*100)
	}
}

// subsequenceRatio 期望的字符里有多少能在结果里按顺序找到(忽略大小写和空格)
func subsequenceRatio(want, got string) float64 {
	w := []rune(strings.ToUpper(strings.ReplaceAll(want, " ", "")))
	g := []rune(strings.ToUpper(strings.ReplaceAll(got, " ", "")))
	hit, j := 0, 0
	for _, r := range w {
		for j < len(g) && g[j] != r {
			j++
		}
		if j < len(g) {
			hit++
			j++
		}
	}
	if len(w) == 0 {
		return 0
	}
	return float64(hit) / float64(len(w))
}

// renderText 画一行大字。优先用系统里的 Arial,点阵字放大之后是方块字,引擎认得吃力
func renderText(s string) []byte {
	face := font.Face(basicfont.Face7x13)
	scale := 8
	if ttf, err := os.ReadFile(filepath.Join(os.Getenv("WINDIR"), "Fonts", "arial.ttf")); err == nil {
		if f, err := opentype.Parse(ttf); err == nil {
			if fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 48, DPI: 72}); err == nil {
				face = fc
				scale = 1
			}
		}
	}
	width := font.MeasureString(face, s).Ceil() + 40
	height := face.Metrics().Height.Ceil() + 40
	small := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(small, small.Bounds(), image.White, image.Point{}, draw.Src)
	d := font.Drawer{
		Dst:  small,
		Src:  image.NewUniform(color.Black),
		Face: face,
		Dot:  fixed.P(20, 20+face.Metrics().Ascent.Ceil()),
	}
	d.DrawString(s)

	big := image.NewRGBA(image.Rect(0, 0, width*scale, height*scale))
	for y := 0; y < big.Bounds().Dy(); y++ {
		for x := 0; x < big.Bounds().Dx(); x++ {
			big.Set(x, y, small.At(x/scale, y/scale))
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, big)
	return buf.Bytes()
}
