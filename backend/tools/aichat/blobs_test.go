package aichat

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 这些用例都会读写 dataDir()。指到临时目录去,别碰用户真实的 ~/.toolforge
func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home) // Windows
	t.Setenv("HOME", home)        // 其余平台
	return home
}

func TestBlobRoundTrip(t *testing.T) {
	useTempHome(t)
	raw := []byte("\x89PNG\r\n\x1a\n假装这是一张图")
	b64 := base64.StdEncoding.EncodeToString(raw)

	ref, err := putBlob("image/png", b64)
	if err != nil {
		t.Fatalf("写 blob 失败: %v", err)
	}
	if !strings.HasSuffix(ref, ".png") {
		t.Errorf("引用应该带扩展名(HTTP 处理器靠它定 Content-Type): %q", ref)
	}
	if !blobRefPattern.MatchString(ref) {
		t.Errorf("引用形态不合法: %q", ref)
	}

	got, err := readBlob(ref)
	if err != nil || got != b64 {
		t.Fatalf("读回来的内容对不上: err=%v", err)
	}

	// 内容寻址:同样的内容存两次,磁盘上只有一份
	ref2, _ := putBlob("image/png", b64)
	if ref2 != ref {
		t.Errorf("同样的内容应该得到同一个引用: %q vs %q", ref, ref2)
	}
	bd, _ := blobDir()
	files, _ := os.ReadDir(bd)
	if len(files) != 1 {
		t.Errorf("同样的内容不该存两份,目录里有 %d 个文件", len(files))
	}
}

// 引用会被拼进文件路径,也会被拼进 HTTP 路由。松一点就是任意文件读取
func TestBlobPathRejectsTraversal(t *testing.T) {
	useTempHome(t)
	for _, bad := range []string{
		"../../../../etc/passwd",
		"..\\..\\windows\\system32\\config\\sam",
		"abc.png",                        // 不是 64 位哈希
		strings.Repeat("g", 64) + ".png", // 非十六进制
		strings.Repeat("a", 64),          // 没有扩展名
		strings.Repeat("a", 64) + ".png/../../x",
		"",
	} {
		if _, err := BlobPath(bad); err == nil {
			t.Errorf("这个引用必须被拒绝: %q", bad)
		}
	}
}

func TestExternalizeAndHydrate(t *testing.T) {
	useTempHome(t)
	imgB64 := base64.StdEncoding.EncodeToString([]byte("图片内容"))
	pdfB64 := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 假的"))
	msgs := []Message{{
		Role:   RoleUser,
		Images: []ImageBlock{{MimeType: "image/png", Data: imgB64}},
		Files: []FileBlock{{
			Name: "a.pdf", MimeType: "application/pdf", Data: pdfB64,
			Text: "提取出来的文字", // Text 不外置:模型每轮都要看它
		}},
	}}

	if !externalizeMessages(msgs) {
		t.Fatal("有内联数据,应该报告发生了改动")
	}
	if msgs[0].Images[0].Data != "" || msgs[0].Images[0].Ref == "" {
		t.Errorf("图片没被外置: %+v", msgs[0].Images[0])
	}
	if msgs[0].Files[0].Data != "" || msgs[0].Files[0].Ref == "" {
		t.Errorf("附件没被外置: %+v", msgs[0].Files[0])
	}
	if msgs[0].Files[0].Text != "提取出来的文字" {
		t.Error("Text 不该被动 —— 它不大,而且每轮都要发给模型")
	}
	// 已经外置过的再走一遍不该有改动(迁移重跑时靠这个避免空转重写)
	if externalizeMessages(msgs) {
		t.Error("没有内联数据时不该报告改动")
	}

	got := hydrateMessages(msgs)
	if got[0].Images[0].Data != imgB64 {
		t.Error("图片没回填")
	}
	if got[0].Files[0].Data != pdfB64 {
		t.Error("附件没回填")
	}
	// 回填拿到的必须是副本:就地填回去的话,这些 base64 会跟着下一次
	// saveConversation 再走一遍外置,白忙一趟
	if msgs[0].Images[0].Data != "" {
		t.Error("hydrate 不该改动传进去的那份")
	}
}

func TestGCKeepsReferencedAndMarker(t *testing.T) {
	home := useTempHome(t)
	d, _ := dataDir()
	bd, _ := blobDir()

	keep, _ := putBlob("image/png", base64.StdEncoding.EncodeToString([]byte("还在用的图")))
	drop, _ := putBlob("image/png", base64.StdEncoding.EncodeToString([]byte("没人要的图")))
	marker := filepath.Join(bd, migrateMarker)
	if err := os.WriteFile(marker, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 一条引用了 keep 的会话
	c := &Conversation{ID: "c1", Title: "x", Messages: []Message{
		{Role: RoleUser, Images: []ImageBlock{{Ref: keep, MimeType: "image/png"}}},
	}}
	if err := writeJSONAtomic(filepath.Join(d, "conversations", "c1.json"), c); err != nil {
		t.Fatal(err)
	}

	// 把两个 blob 和标记的时间推老,越过宽限期
	old := time.Now().Add(-time.Hour)
	for _, n := range []string{keep, drop, migrateMarker} {
		_ = os.Chtimes(filepath.Join(bd, n), old, old)
	}

	gcBlobs()

	if _, err := os.Stat(filepath.Join(bd, keep)); err != nil {
		t.Error("被引用的 blob 不该被删")
	}
	if _, err := os.Stat(filepath.Join(bd, drop)); !os.IsNotExist(err) {
		t.Error("没人引用的 blob 应该被删掉")
	}
	// 标记也住在这个目录里,而且当然没有会话引用它。
	// 删了的话迁移每次启动都重跑,永远落不下标记
	if _, err := os.Stat(marker); err != nil {
		t.Error("迁移完成标记不该被回收")
	}
	_ = home
}

func TestGCSkipsFreshBlobs(t *testing.T) {
	useTempHome(t)
	bd, _ := blobDir()
	fresh, _ := putBlob("image/png", base64.StdEncoding.EncodeToString([]byte("刚存下还没人引用")))
	// 场景:图片已经落盘,引用它的会话还没写完。这时候扫一遍"没人引用"就会删掉它
	gcBlobs()
	if _, err := os.Stat(filepath.Join(bd, fresh)); err != nil {
		t.Error("宽限期内的 blob 不该被回收")
	}
}

func TestMigrateInlineBlobs(t *testing.T) {
	useTempHome(t)
	d, _ := dataDir()
	// 用一张有真实体量的"图"。几十字节的样本证明不了什么 —— 引用本身
	// 就有 68 个字符,比那点 base64 还长,反而会让文件变大。
	// 这个功能针对的是几百 KB 到几 MB 的图片,样本也得是那个量级
	b64 := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("图片数据", 30000)))
	path := filepath.Join(d, "conversations", "old.json")
	if err := writeJSONAtomic(path, &Conversation{
		ID: "old", Title: "老会话", Messages: []Message{
			{Role: RoleAssistant, Images: []ImageBlock{{MimeType: "image/png", Data: b64}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)

	MigrateInlineBlobs()

	var c Conversation
	if err := readJSON(path, &c); err != nil {
		t.Fatal(err)
	}
	if c.Messages[0].Images[0].Data != "" || c.Messages[0].Images[0].Ref == "" {
		t.Fatalf("老会话没被迁移: %+v", c.Messages[0].Images[0])
	}
	after, _ := os.Stat(path)
	if after.Size() >= before.Size() {
		t.Errorf("迁移后文件应该变小: %d → %d", before.Size(), after.Size())
	}
	// 数据还在,只是换了地方
	if got, err := readBlob(c.Messages[0].Images[0].Ref); err != nil || got != b64 {
		t.Errorf("迁移把数据弄丢了: err=%v", err)
	}
	// 标记落下之后不再重扫
	bd, _ := blobDir()
	if _, err := os.Stat(filepath.Join(bd, migrateMarker)); err != nil {
		t.Error("迁移完成后应该落一个标记")
	}
}
