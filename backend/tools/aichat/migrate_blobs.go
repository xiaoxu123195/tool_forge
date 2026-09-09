package aichat

import (
	"os"
	"path/filepath"
)

// migrateMarker 迁移完成的标记。有它就不再扫 —— 每次启动都把所有会话读一遍
// 只为确认"上次已经搬完了",纯属浪费
const migrateMarker = ".blobs-migrated-v1"

// MigrateInlineBlobs 把老会话里内联的 base64 搬进 blob 目录。
//
// 一次性的,做完落一个标记文件。设计上有几处是刻意保守的:
//
//   - 逐个会话处理,一条失败不影响其余。老数据里什么形状都可能有,
//     不能让一条坏文件把整次迁移卡死。
//   - 先写 blob 再重写会话 JSON,而且 JSON 是原子替换(.tmp + rename)。
//     任何一步中断,原会话文件都还是完整的老样子 —— 最坏结果是多了几个
//     没人引用的 blob,下次 gcBlobs 会清掉。
//   - 一个字节都没搬动的会话不重写:没必要为了"确认无事发生"去改文件的
//     修改时间,那会让会话列表的排序莫名其妙地全变了。
func MigrateInlineBlobs() {
	d, err := dataDir()
	if err != nil {
		return
	}
	bd, err := blobDir()
	if err != nil {
		return
	}
	marker := filepath.Join(bd, migrateMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}

	convDir := filepath.Join(d, "conversations")
	entries, err := os.ReadDir(convDir)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.WriteFile(marker, []byte("ok"), 0o600) // 一条会话都没有,也算搬完了
		}
		return
	}
	failed := false
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(convDir, e.Name())
		var c Conversation
		if err := readJSON(path, &c); err != nil || c.ID == "" {
			failed = true
			continue
		}
		if !externalizeMessages(c.Messages) {
			continue // 本来就没有内联数据
		}
		if err := writeJSONAtomic(path, &c); err != nil {
			failed = true
		}
	}
	// 有失败就不落标记:下次启动再试一遍。已经搬过的那些会话这时没有内联数据了,
	// 重跑等于空转,不会重复搬运
	if !failed {
		_ = os.WriteFile(marker, []byte("ok"), 0o600)
	}
}
