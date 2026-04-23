package claudeinsight

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

// manifestFileName ZIP 包里固定的清单文件名
const manifestFileName = "manifest.json"

// sessionFileNamePattern 用于校验/生成 session 子文件名,避免解压越权。
var sessionFileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.jsonl$`)

// manifest ZIP 的自描述清单,便于跨机器/版本导入。
type manifest struct {
	Version    int               `json:"version"`
	ExportedAt string            `json:"exported_at"`
	Sessions   []manifestSession `json:"sessions"`
}

type manifestSession struct {
	SessionID string `json:"session_id"`
	Project   string `json:"project"` // 原 cwd 路径,用于导入时还原目录
	File      string `json:"file"`    // 在 ZIP 内的文件名 (xxx.jsonl)
	Size      int64  `json:"size"`
	Messages  int    `json:"messages"`
}

// ExportResult 导出完成后的结果
type ExportResult struct {
	ZipPath  string `json:"zip_path"`
	Sessions int    `json:"sessions"`
	Size     int64  `json:"size"`
}

// ImportResult 导入完成后的结果
type ImportResult struct {
	Imported  int      `json:"imported"` // 成功写入的 session 数
	Skipped   int      `json:"skipped"`  // 已存在跳过的数量
	Projects  []string `json:"projects"` // 涉及的 projects 目录(去重)
	ClaudeDir string   `json:"claude_dir"`
}

// ExportSessions 把指定的若干会话文件打包成 ZIP。
// filePaths 必须是 ~/.claude/projects 下真实存在的 .jsonl 文件绝对路径。
func ExportSessions(claudeDir string, filePaths []string, destZip string) (*ExportResult, error) {
	if len(filePaths) == 0 {
		return nil, errors.New("至少选择一个会话")
	}
	if strings.TrimSpace(destZip) == "" {
		return nil, errors.New("目标 zip 路径不能为空")
	}
	if !strings.HasSuffix(strings.ToLower(destZip), ".zip") {
		destZip += ".zip"
	}

	dir, err := resolveClaudeDir(claudeDir)
	if err != nil {
		return nil, err
	}
	projectsDir := filepath.Join(dir, "projects")

	// 全部 file 必须都在 projects 下,防路径逃逸
	for _, p := range filePaths {
		if err := ensureUnder(projectsDir, p); err != nil {
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
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
	}

	nameCount := map[string]int{}
	for _, src := range filePaths {
		acc, err := scanSessionFile(src)
		if err != nil {
			return nil, fmt.Errorf("读取失败 %s: %w", src, err)
		}
		if acc == nil {
			continue
		}
		// 去重命名,同名 session 追加 -2 -3 ...
		base := safeSessionFileName(acc.id, src)
		name := base
		if n := nameCount[base]; n > 0 {
			name = fmt.Sprintf("%s-%d.jsonl", strings.TrimSuffix(base, ".jsonl"), n+1)
		}
		nameCount[base]++

		info, err := os.Stat(src)
		if err != nil {
			return nil, err
		}
		fw, err := zw.Create(name)
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
			File:      name,
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
	return &ExportResult{
		ZipPath:  destZip,
		Sessions: len(mf.Sessions),
		Size:     sizeOr0(fi),
	}, nil
}

func sizeOr0(fi os.FileInfo) int64 {
	if fi == nil {
		return 0
	}
	return fi.Size()
}

// safeSessionFileName 生成 zip 内条目名:优先用 session id,否则用源文件名。
func safeSessionFileName(sessionID, srcPath string) string {
	if sessionID != "" && sessionFileNamePattern.MatchString(sessionID+".jsonl") {
		return sessionID + ".jsonl"
	}
	base := filepath.Base(srcPath)
	if sessionFileNamePattern.MatchString(base) {
		return base
	}
	// fallback:强制清理
	clean := regexp.MustCompile(`[^A-Za-z0-9._-]`).ReplaceAllString(base, "-")
	if !strings.HasSuffix(clean, ".jsonl") {
		clean += ".jsonl"
	}
	if clean == "" || clean == ".jsonl" {
		clean = "session.jsonl"
	}
	return clean
}

// ImportSessions 从 ZIP 恢复 session 到 ~/.claude/projects/<project-dir>/。
// project-dir 名按 Claude Code 的规则把 cwd 的 \:/ 替换为 -。
// 若目标文件已存在,跳过(不覆盖),在结果里 +Skipped。
func ImportSessions(claudeDir, zipPath string) (*ImportResult, error) {
	if strings.TrimSpace(zipPath) == "" {
		return nil, errors.New("zip 路径不能为空")
	}
	dir, err := resolveClaudeDir(claudeDir)
	if err != nil {
		return nil, err
	}
	projectsDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		return nil, err
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()

	var mf manifest
	var mfFound bool
	// 先找 manifest
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
			mfFound = true
			break
		}
	}
	if !mfFound {
		return nil, errors.New("zip 里没找到 manifest.json,不是有效的 Tool Forge 会话导出包")
	}

	res := &ImportResult{ClaudeDir: dir}
	projectSet := map[string]struct{}{}

	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[f.Name] = f
	}

	for _, s := range mf.Sessions {
		entry, ok := byName[s.File]
		if !ok {
			return nil, fmt.Errorf("manifest 引用了不存在的条目: %s", s.File)
		}
		if !sessionFileNamePattern.MatchString(filepath.Base(s.File)) {
			return nil, fmt.Errorf("非法文件名: %s", s.File)
		}
		projectDir := claudeProjectDirName(s.Project)
		if projectDir == "" {
			projectDir = "_imported_"
		}
		targetDir := filepath.Join(projectsDir, projectDir)
		if err := ensureUnder(projectsDir, targetDir); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return nil, err
		}
		target := filepath.Join(targetDir, filepath.Base(s.File))
		if _, err := os.Stat(target); err == nil {
			// 已存在,跳过
			res.Skipped++
			projectSet[projectDir] = struct{}{}
			continue
		}
		if err := writeZipEntry(entry, target); err != nil {
			return nil, err
		}
		res.Imported++
		projectSet[projectDir] = struct{}{}
	}

	for p := range projectSet {
		res.Projects = append(res.Projects, p)
	}
	return res, nil
}

// DeleteSession 删除一个会话 .jsonl。必须位于 claudeDir/projects 下，防越权。
func DeleteSession(claudeDir, filePath string) error {
	if strings.TrimSpace(filePath) == "" {
		return errors.New("文件路径不能为空")
	}
	dir, err := resolveClaudeDir(claudeDir)
	if err != nil {
		return err
	}
	projectsDir := filepath.Join(dir, "projects")
	if err := ensureUnder(projectsDir, filePath); err != nil {
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

// claudeProjectDirName 把 cwd 路径转为 Claude Code 风格的目录名:
// 把 \ / : 统一替换为 -。
// 例: "D:\\go_pro\\new_tools\\tool_forge" -> "D--go_pro-new_tools-tool_forge"
func claudeProjectDirName(project string) string {
	if project == "" {
		return ""
	}
	replacer := strings.NewReplacer(`\`, "-", "/", "-", ":", "-")
	return replacer.Replace(project)
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
