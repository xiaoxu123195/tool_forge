package aiconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 配置文件的读写。
//
// 只做原样读写文本,绝不「解析 → 改 → 再序列化」。那些文件是别的程序在维护的,
// 里面有手写的注释、缩进、键的顺序;我们用 Go 的结构体转一圈再写回去,
// 认不出的字段会消失,TOML 的注释也会全没。用户会以为只是改了一个值。
//
// 写之前留一份 .bak。改的是别家程序的配置,改坏了对方可能起不来。

const (
	// maxFileSize 配置文件都很小;.claude.json 大是因为塞了统计数据,
	// 但也就几百 KB。给 8 MB 足够,同时挡住"不小心点开一个大文件"
	maxFileSize = 8 << 20
)

// editableExts 能在本页编辑的扩展名。
// 限定在这几种,是因为它们都是纯文本、改坏了人眼能看出来
var editableExts = map[string]bool{
	".json":  true,
	".md":    true,
	".toml":  true,
	".rules": true,
	".txt":   true,
	".yaml":  true,
	".yml":   true,
}

// FileContent 一个配置文件的内容
type FileContent struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updatedAt"`
	// Editable 这个文件能不能在本页改
	Editable bool `json:"editable"`
	// Reason Editable 为假时的原因,直接显示给用户
	Reason string `json:"reason,omitempty"`
}

// allowRoots 允许触碰的根目录。
//
// 不做这层限制的话,这两个绑定就成了"任意路径读写"——前端传什么就读什么。
// 这个应用还同时是个 MCP server,那等于给 agent 开了一扇读写整个磁盘的门。
func allowRoots(h Home) ([]string, error) {
	home, err := h.resolve()
	if err != nil {
		return nil, err
	}
	return []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".toolforge"),
		filepath.Join(home, ".claude.json"), // 这个是文件不是目录
	}, nil
}

// checkPath 校验路径在允许范围内,返回清理过的绝对路径
func checkPath(h Home, p string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(p))
	if err != nil {
		return "", err
	}
	// 先解软链再比:否则 ~/.claude/x 指向 C:\Windows 时能绕过去
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	roots, err := allowRoots(h)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		if realRoot, err := filepath.EvalSymlinks(root); err == nil {
			root = realRoot
		}
		if abs == root {
			return abs, nil
		}
		// 必须带分隔符比,否则 ~/.claudeXXX 会被当成 ~/.claude 底下的
		if strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("这个路径不在本机 AI 配置的范围内: %s", abs)
}

// ReadFile 读一个配置文件
func ReadFile(h Home, path string) (*FileContent, error) {
	abs, err := checkPath(h, path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("这是一个目录: %s", abs)
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("文件太大(%d 字节),本页不打开", info.Size())
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	out := &FileContent{
		Path:      abs,
		Content:   string(data),
		Size:      info.Size(),
		UpdatedAt: info.ModTime().Format("2006-01-02 15:04:05"),
		Editable:  editableExts[strings.ToLower(filepath.Ext(abs))],
	}
	if !out.Editable {
		out.Reason = "这个扩展名不在可编辑范围内,只读"
	}
	return out, nil
}

// WriteFile 原样写回,写前留一份 .bak。
//
// 不校验内容格式:校验就得先解析,而我们恰恰不想解析。写坏了由用户自己
// 从 .bak 恢复 —— 这比我们自作主张改写他的文件要好。
func WriteFile(h Home, path, content string) error {
	abs, err := checkPath(h, path)
	if err != nil {
		return err
	}
	if !editableExts[strings.ToLower(filepath.Ext(abs))] {
		return fmt.Errorf("这个扩展名不允许写入: %s", filepath.Ext(abs))
	}
	if len(content) > maxFileSize {
		return fmt.Errorf("内容太大")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("这是一个目录")
	}

	if _, err := backupFile(abs); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), info.Mode().Perm())
}

// backupFile 在旁边留一份副本,返回副本路径。
//
// 改的是别家程序的配置,改坏了对方可能起不来。带时间戳,不覆盖上一次的备份 ——
// 连续改错两次时,单一 .bak 里存的已经是第一次改错的结果,恢复了等于没恢复
func backupFile(abs string) (string, error) {
	old, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("读不到原文件,没有动它: %w", err)
	}
	bak := fmt.Sprintf("%s.%s.bak", abs, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(bak, old, 0o644); err != nil {
		return "", fmt.Errorf("备份失败,没有动原文件: %w", err)
	}
	return bak, nil
}
