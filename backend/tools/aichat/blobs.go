package aichat

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// 图片和附件不再以 base64 塞在会话 JSON 里,而是按内容哈希单独落一个文件,
// 会话里只留一个引用。
//
// 起因是实测:15 条会话 4.7MB,其中约 90% 是 base64;有一条 3.4MB 的会话
// 正文一个字都没有,全是图。内联带来四个连锁后果 ——
// 打开会话要把几 MB base64 过一遍前端桥、跨会话搜索每次把它们全读一遍、
// 每答一句重写整个文件、导出内嵌图片直接爆。
//
// 按内容哈希命名顺带解决重复:同一张图被重新生成、被分叉复制,磁盘上只有一份。

// blobRefPattern 合法的引用形态:64 位十六进制 + 扩展名。
//
// 这个校验是安全边界而不是格式洁癖 —— 引用会被拼进文件路径,也会被拼进
// HTTP 路由。松一点就是任意文件读取。
var blobRefPattern = regexp.MustCompile(`^[0-9a-f]{64}\.[a-z0-9]{1,8}$`)

func blobDir() (string, error) {
	d, err := dataDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(d, "blobs")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// BlobPath 把引用解析成磁盘路径;引用不合法返回错误。
// 导出给 main.go 的 HTTP 处理器用 —— 它同样必须走这道校验。
func BlobPath(ref string) (string, error) {
	if !blobRefPattern.MatchString(ref) {
		return "", fmt.Errorf("非法的附件引用")
	}
	d, err := blobDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, ref), nil
}

// putBlob 把 base64 数据落成一个文件,返回引用。同样的内容只会有一份。
func putBlob(mime, b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("附件不是合法的 base64: %w", err)
	}
	sum := sha256.Sum256(raw)
	ref := hex.EncodeToString(sum[:]) + extForMime(mime)
	path, err := BlobPath(ref)
	if err != nil {
		return "", err
	}
	// 已经有了就不重写:内容寻址,同名必然同内容
	if _, err := os.Stat(path); err == nil {
		return ref, nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return ref, nil
}

// readBlob 按引用读回 base64
func readBlob(ref string) (string, error) {
	path, err := BlobPath(ref)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// externalizeMessages 把消息里内联的 base64 挪进 blob,原地改成引用。
// 返回是否真的改动过(迁移时用它决定要不要重写文件)。
//
// 落盘前的最后一道:放在 saveConversation 里,不管哪条代码路径存的会话,
// 都不可能再把 base64 写进 JSON。
func externalizeMessages(msgs []Message) bool {
	changed := false
	for i := range msgs {
		for j := range msgs[i].Images {
			img := &msgs[i].Images[j]
			if img.Data == "" || img.Ref != "" {
				continue
			}
			ref, err := putBlob(img.MimeType, img.Data)
			if err != nil {
				continue // 存不进去就保持内联:宁可文件大,也不能把用户的图弄丢
			}
			img.Ref, img.Data = ref, ""
			changed = true
		}
		for j := range msgs[i].Files {
			f := &msgs[i].Files[j]
			if f.Data == "" || f.Ref != "" {
				continue
			}
			ref, err := putBlob(f.MimeType, f.Data)
			if err != nil {
				continue
			}
			f.Ref, f.Data = ref, ""
			changed = true
		}
	}
	return changed
}

// hydrateMessages 反过来:把引用读回成内联 base64。
//
// 只在"要把消息发给模型"和"要导出成自包含文件"时用 —— 协议层需要的是真数据。
// 返回的是副本,不动调用方那份:就地填回去的话,这些 base64 会跟着
// 下一次 saveConversation 再走一遍外置,白忙一趟。
func hydrateMessages(msgs []Message) []Message {
	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if len(out[i].Images) > 0 {
			imgs := make([]ImageBlock, len(out[i].Images))
			copy(imgs, out[i].Images)
			for j := range imgs {
				if imgs[j].Ref != "" && imgs[j].Data == "" {
					if b64, err := readBlob(imgs[j].Ref); err == nil {
						imgs[j].Data = b64
					}
				}
			}
			out[i].Images = imgs
		}
		if len(out[i].Files) > 0 {
			files := make([]FileBlock, len(out[i].Files))
			copy(files, out[i].Files)
			for j := range files {
				if files[j].Ref != "" && files[j].Data == "" {
					if b64, err := readBlob(files[j].Ref); err == nil {
						files[j].Data = b64
					}
				}
			}
			out[i].Files = files
		}
	}
	return out
}

// extForMime 由 MIME 推一个扩展名。
//
// 扩展名不只是好看:HTTP 处理器靠它决定 Content-Type,而且用户在文件管理器里
// 直接双击就能打开 —— 一堆没有后缀的哈希文件对排查毫无帮助。
func extForMime(mime string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	switch m {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	case "application/pdf":
		return ".pdf"
	}
	return ".bin"
}

// MimeForBlobRef 由引用的扩展名反推 Content-Type(HTTP 处理器用)
func MimeForBlobRef(ref string) string {
	switch strings.ToLower(filepath.Ext(ref)) {
	case ".png":
		return "image/png"
	case ".jpg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	}
	return "application/octet-stream"
}

// blobGraceWindow 刚写下的 blob 不参与回收。
//
// 要挡的场景很具体:一条消息的图片已经落盘,而引用它的会话还没写完
// (用户正在发送、正在分叉)。这时候扫一遍"没人引用",就会把它删掉。
const blobGraceWindow = 10 * time.Minute

// GCBlobs 导出给启动流程用的回收入口(包内的调用点直接用 gcBlobs)
func GCBlobs() { gcBlobs() }

// gcBlobs 删掉没有任何会话引用的 blob。
//
// 内容寻址意味着一个 blob 可能被多条会话共用(分叉、同一张图重新生成),
// 所以不能在删会话时顺手删掉它的附件 —— 只能扫全量再对账。
// 会话数是几十条这个量级,扫一遍很便宜。
func gcBlobs() {
	d, err := dataDir()
	if err != nil {
		return
	}
	bd, err := blobDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(filepath.Join(d, "conversations"))
	if err != nil {
		return
	}
	used := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var c Conversation
		if err := readJSON(filepath.Join(d, "conversations", e.Name()), &c); err != nil {
			// 读不动的会话文件:当成"它引用了一切",这一轮什么都不删。
			// 宁可留下垃圾,也不能因为一次读失败就把还在用的附件清掉
			return
		}
		for _, m := range c.Messages {
			for _, img := range m.Images {
				if img.Ref != "" {
					used[img.Ref] = true
				}
			}
			for _, f := range m.Files {
				if f.Ref != "" {
					used[f.Ref] = true
				}
			}
		}
	}

	blobs, err := os.ReadDir(bd)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-blobGraceWindow)
	for _, b := range blobs {
		if b.IsDir() || used[b.Name()] {
			continue
		}
		// 只回收长得像 blob 的文件。这个目录里还住着迁移完成标记
		// (.blobs-migrated-v1),它当然不会被任何会话引用 —— 按"没人引用"
		// 的规则会被删掉,于是迁移每次启动都重跑一遍,永远落不下标记
		if !blobRefPattern.MatchString(b.Name()) {
			continue
		}
		info, err := b.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(bd, b.Name()))
	}
}
