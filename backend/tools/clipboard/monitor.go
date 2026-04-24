package clipboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	clipx "golang.design/x/clipboard"
	"golang.org/x/image/draw"
)

// startMonitor 阻塞地监听文本与图片剪贴板变化,变化时回调 onNew。
// 调用方应在独立 goroutine 中执行;通过 ctx 取消。
func startMonitor(ctx context.Context, store *Store, onNew func(Item)) {
	if err := clipx.Init(); err != nil {
		log.Printf("[clipboard] init failed: %v", err)
		return
	}
	log.Printf("[clipboard] monitor started, watching FmtText + FmtImage")
	textCh := clipx.Watch(ctx, clipx.FmtText)
	imageCh := clipx.Watch(ctx, clipx.FmtImage)
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-textCh:
			if !ok {
				textCh = nil
				continue
			}
			cfg := store.Config()
			if !cfg.Enabled {
				continue
			}
			text := string(data)
			if strings.TrimSpace(text) == "" {
				continue
			}
			if cfg.MaxTextBytes > 0 && len(data) > cfg.MaxTextBytes {
				log.Printf("[clipboard] text dropped: %d bytes > limit %d", len(data), cfg.MaxTextBytes)
				continue
			}
			if store.LatestKindContent(KindText) == text {
				continue
			}
			item := buildTextItem(text)
			if added, ok := store.Add(item); ok {
				onNew(added)
			}
		case data, ok := <-imageCh:
			if !ok {
				imageCh = nil
				continue
			}
			cfg := store.Config()
			if !cfg.Enabled {
				continue
			}
			log.Printf("[clipboard] image event received: %d bytes", len(data))
			if len(data) == 0 {
				continue
			}
			if cfg.MaxImageBytes > 0 && len(data) > cfg.MaxImageBytes {
				log.Printf("[clipboard] image dropped: %d bytes > limit %d", len(data), cfg.MaxImageBytes)
				continue
			}
			item, err := buildImageItem(store, data)
			if err != nil {
				log.Printf("[clipboard] save image failed: %v (data head=%x)", err, data[:min(16, len(data))])
				continue
			}
			if added, ok := store.Add(item); ok {
				onNew(added)
			}
		}
	}
}

func buildTextItem(text string) Item {
	now := time.Now()
	id := fmt.Sprintf("%d-%s", now.UnixNano(), shortHash(text))
	return Item{
		ID:        id,
		Kind:      KindText,
		Text:      text,
		Preview:   makePreview(text, 280),
		SizeBytes: len(text),
		CreatedAt: now.UnixMilli(),
	}
}

// buildImageItem 把 PNG 字节落盘 + 生成缩略图 dataURL
func buildImageItem(store *Store, data []byte) (Item, error) {
	now := time.Now()
	hash := shortHash(string(data[:min(64, len(data))])) // 用前 64 字节做粗略 hash,避免大图全量
	id := fmt.Sprintf("%d-%s", now.UnixNano(), hash)
	dst := filepath.Join(store.ImagesDir(), id+".png")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return Item{}, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Item{}, err
	}
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	thumb, err := encodeThumbnail(img, 240)
	if err != nil {
		return Item{}, err
	}
	return Item{
		ID:          id,
		Kind:        KindImage,
		ImagePath:   dst,
		Thumbnail:   thumb,
		ImageWidth:  w,
		ImageHeight: h,
		SizeBytes:   len(data),
		CreatedAt:   now.UnixMilli(),
	}, nil
}

// encodeThumbnail 等比缩放至最长边 maxSide,输出 PNG dataURL
func encodeThumbnail(src image.Image, maxSide int) (string, error) {
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	scale := 1.0
	if w > maxSide || h > maxSide {
		if w >= h {
			scale = float64(maxSide) / float64(w)
		} else {
			scale = float64(maxSide) / float64(h)
		}
	}
	dstW := int(float64(w) * scale)
	dstH := int(float64(h) * scale)
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func makePreview(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
