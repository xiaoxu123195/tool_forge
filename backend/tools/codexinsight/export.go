package codexinsight

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const manifestFileName = "manifest.json"

// rolloutPattern Codex session 文件名匹配 rollout-YYYY-MM-DD...-uuid.jsonl
var rolloutPattern = regexp.MustCompile(`^rollout-\d{4}-\d{2}-\d{2}[\w.-]*\.jsonl$`)

type manifest struct {
	Version    int               `json:"version"`
	Flavor     string            `json:"flavor"` // "codex"
	ExportedAt string            `json:"exported_at"`
	Sessions   []manifestSession `json:"sessions"`
}

type manifestSession struct {
	SessionID string `json:"session_id"`
	Project   string `json:"project"`
	File      string `json:"file"`     // zip 内文件名
	DateDir   string `json:"date_dir"` // "YYYY/MM/DD"
	Size      int64  `json:"size"`
	Messages  int    `json:"messages"`
}

// ExportResult 导出结果
type ExportResult struct {
	ZipPath  string `json:"zip_path"`
	Sessions int    `json:"sessions"`
	Size     int64  `json:"size"`
}

// ImportResult 导入结果
type ImportResult struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Dirs     []string `json:"dirs"`
	CodexDir string   `json:"codex_dir"`
}

// ExportSessions 把指定的若干会话文件打包成 ZIP。
func ExportSessions(codexDir string, filePaths []string, destZip string) (*ExportResult, error) {
	if len(filePaths) == 0 {
		return nil, errors.New("至少选择一个会话")
	}
	if strings.TrimSpace(destZip) == "" {
		return nil, errors.New("目标 zip 路径不能为空")
	}
	if !strings.HasSuffix(strings.ToLower(destZip), ".zip") {
		destZip += ".zip"
	}

	dir, err := resolveCodexDir(codexDir)
	if err != nil {
		return nil, err
	}
	sessionsDir := filepath.Join(dir, "sessions")

	// 校验所有 filePaths 必须位于 sessions/ 下
	for _, p := range filePaths {
		if err := ensureUnder(sessionsDir, p); err != nil {
			return nil, err
		}
	}

	out, err := os.Create(destZip)
	if err != nil {
		return nil, err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()

	mf := manifest{
		Version:    1,
		Flavor:     "codex",
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, src := range filePaths {
		acc, err := scanSessionFile(src)
		if err != nil {
			return nil, fmt.Errorf("读取失败 %s: %w", src, err)
		}
		if acc == nil {
			continue
		}
		name := filepath.Base(src)
		if !rolloutPattern.MatchString(name) {
			continue
		}
		info, err := os.Stat(src)
		if err != nil {
			return nil, err
		}
		// 从原路径提取 YYYY/MM/DD 层级,便于导入时恢复
		rel, err := filepath.Rel(sessionsDir, src)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		dateDir := filepath.ToSlash(filepath.Dir(rel))

		zipEntryName := filepath.ToSlash(rel) // 保持相对路径结构
		fw, err := zw.Create(zipEntryName)
		if err != nil {
			return nil, err
		}
		sf, err := os.Open(src)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(fw, sf); err != nil {
			sf.Close()
			return nil, err
		}
		sf.Close()

		mf.Sessions = append(mf.Sessions, manifestSession{
			SessionID: acc.id,
			Project:   acc.project,
			File:      zipEntryName,
			DateDir:   dateDir,
			Size:      info.Size(),
			Messages:  acc.messages,
		})
	}

	mfBytes, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return nil, err
	}
	mfWriter, err := zw.Create(manifestFileName)
	if err != nil {
		return nil, err
	}
	if _, err := mfWriter.Write(mfBytes); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	fi, _ := os.Stat(destZip)
	size := int64(0)
	if fi != nil {
		size = fi.Size()
	}
	return &ExportResult{
		ZipPath:  destZip,
		Sessions: len(mf.Sessions),
		Size:     size,
	}, nil
}

// ImportSessions 从 ZIP 恢复 session 到 ~/.codex/sessions/YYYY/MM/DD/。
// manifest 里的 date_dir 直接决定目标子目录;已存在文件跳过。
func ImportSessions(codexDir, zipPath string) (*ImportResult, error) {
	if strings.TrimSpace(zipPath) == "" {
		return nil, errors.New("zip 路径不能为空")
	}
	dir, err := resolveCodexDir(codexDir)
	if err != nil {
		return nil, err
	}
	sessionsDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return nil, err
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()

	var mf manifest
	var found bool
	for _, f := range zr.File {
		if f.Name == manifestFileName {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(data, &mf); err != nil {
				return nil, fmt.Errorf("manifest.json 解析失败: %w", err)
			}
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("zip 里没找到 manifest.json")
	}
	if mf.Flavor != "" && mf.Flavor != "codex" {
		return nil, fmt.Errorf("zip flavor=%q,不是 codex 导出包", mf.Flavor)
	}

	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[f.Name] = f
	}

	res := &ImportResult{CodexDir: dir}
	dirSet := map[string]struct{}{}

	for _, s := range mf.Sessions {
		entry, ok := byName[s.File]
		if !ok {
			return nil, fmt.Errorf("zip 里缺少条目: %s", s.File)
		}
		base := filepath.Base(s.File)
		if !rolloutPattern.MatchString(base) {
			return nil, fmt.Errorf("非法 session 文件名: %s", base)
		}
		// 目标子目录:manifest 里的 date_dir(优先) 或从文件名推断
		sub := strings.Trim(s.DateDir, "/\\")
		if sub == "" {
			// fallback:从文件名 rollout-YYYY-MM-DD... 提取
			if len(base) >= 19 {
				sub = base[8:12] + "/" + base[13:15] + "/" + base[16:18]
			} else {
				sub = "imported"
			}
		}
		// 防路径穿越:sub 不能含 ..
		cleanSub := filepath.Clean(sub)
		if strings.Contains(cleanSub, "..") || filepath.IsAbs(cleanSub) {
			return nil, fmt.Errorf("非法 date_dir: %s", sub)
		}
		targetDir := filepath.Join(sessionsDir, cleanSub)
		if err := ensureUnder(sessionsDir, targetDir); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return nil, err
		}
		target := filepath.Join(targetDir, base)
		if _, err := os.Stat(target); err == nil {
			res.Skipped++
			dirSet[cleanSub] = struct{}{}
			continue
		}
		if err := writeZipEntry(entry, target); err != nil {
			return nil, err
		}
		res.Imported++
		dirSet[cleanSub] = struct{}{}
	}
	for k := range dirSet {
		res.Dirs = append(res.Dirs, k)
	}
	return res, nil
}

// DeleteSession 删除一个 Codex 会话 rollout .jsonl。必须位于 codexDir/sessions 下。
func DeleteSession(codexDir, filePath string) error {
	if strings.TrimSpace(filePath) == "" {
		return errors.New("文件路径不能为空")
	}
	dir, err := resolveCodexDir(codexDir)
	if err != nil {
		return err
	}
	sessionsDir := filepath.Join(dir, "sessions")
	if err := ensureUnder(sessionsDir, filePath); err != nil {
		return err
	}
	if !strings.HasSuffix(strings.ToLower(filePath), ".jsonl") {
		return errors.New("只能删除 .jsonl 会话文件")
	}
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("删除失败: %w", err)
	}
	return nil
}

// ensureUnder 防止路径越权;target 必须位于 base 下。
func ensureUnder(base, target string) error {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	baseAbs = filepath.Clean(baseAbs) + string(filepath.Separator)
	targetAbs = filepath.Clean(targetAbs)
	if targetAbs == strings.TrimSuffix(baseAbs, string(filepath.Separator)) {
		return nil
	}
	if !strings.HasPrefix(targetAbs+string(filepath.Separator), baseAbs) {
		return fmt.Errorf("路径越界: %s 不在 %s 下", targetAbs, base)
	}
	return nil
}

func writeZipEntry(entry *zip.File, target string) error {
	rc, err := entry.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, rc)
	return err
}
