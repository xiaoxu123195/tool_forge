package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"tool_forge/backend/system"
	"tool_forge/backend/tools/aistupid"
	"tool_forge/backend/tools/charles"
	"tool_forge/backend/tools/claudeinsight"
	"tool_forge/backend/tools/codexinsight"
	"tool_forge/backend/tools/envscan"
	"tool_forge/backend/tools/forensic"
	"tool_forge/backend/updater"
)

// AppVersion 应用版本号，随 wails.json 同步维护
const AppVersion = "0.1.5"

// AppInfo 应用元信息
type AppInfo struct {
	Version   string `json:"version"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	WailsVer  string `json:"wailsVersion"`
}

// App struct
type App struct {
	ctx      context.Context
	forensic *forensic.Service
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		forensic: forensic.New(),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.forensic.SetContext(ctx)
}

// GetAppInfo 返回应用与运行环境信息
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Version:   AppVersion,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		WailsVer:  "v2.11.0",
	}
}

// ================ Charles ================

// GenerateCharlesKey 根据名称生成 Charles 激活码
func (a *App) GenerateCharlesKey(name string) string {
	return charles.Generate(name)
}

// ================ Forensic ================

// CheckForensic 探测 go-forensic 可执行文件（自定义路径可为空，为空则走 PATH）
func (a *App) CheckForensic(customPath string) forensic.Info {
	return a.forensic.Check(customPath)
}

// SetForensicBinaryPath 配置 go-forensic 路径
func (a *App) SetForensicBinaryPath(path string) {
	a.forensic.SetBinaryPath(path)
}

// RunForensic 启动取证命令，返回 jobID；后续通过 forensic:log / forensic:done 事件推送
func (a *App) RunForensic(args []string) (string, error) {
	return a.forensic.Run(args)
}

// CancelForensic 取消正在执行的任务
func (a *App) CancelForensic(jobID string) error {
	return a.forensic.Cancel(jobID)
}

// ================ System ================

// PickExecutable 选择一个可执行文件
func (a *App) PickExecutable(title string) (string, error) {
	return system.PickFile(a.ctx, system.PickFileOptions{
		Title:       title,
		Extensions:  []string{".exe"},
		DisplayName: "可执行文件",
	})
}

// PickDirectory 选择一个目录
func (a *App) PickDirectory(title, defaultPath string) (string, error) {
	return system.PickDirectory(a.ctx, title, defaultPath)
}

// OpenInExplorer 在系统文件管理器中打开路径
func (a *App) OpenInExplorer(path string) error {
	return system.OpenInExplorer(path)
}

// SavePassword 将密码写入系统凭据库
func (a *App) SavePassword(key, value string) error {
	return system.SavePassword(key, value)
}

// GetPassword 从系统凭据库读取密码
func (a *App) GetPassword(key string) (string, error) {
	return system.GetPassword(key)
}

// DeletePassword 从系统凭据库删除密码
func (a *App) DeletePassword(key string) error {
	return system.DeletePassword(key)
}

// ================ EnvScan ================

// ScanEnvironments 扫描本机开发者工具；未安装的条目不返回。
func (a *App) ScanEnvironments() envscan.ScanReport {
	return envscan.Scan(a.ctx)
}

// ================ AI Stupid Level (Drift Monitor) ================

// FetchAIStupidDrift 拉取 aistupidlevel.info 的最新模型漂移数据（CUSUM 检测）。
// 前端进入工具时调用一次，点击刷新再次调用。
func (a *App) FetchAIStupidDrift() (*aistupid.DriftBatchResponse, error) {
	return aistupid.FetchDrift(a.ctx)
}

// ================ Claude Insight ================

// BuildClaudeDashboard 扫描本机 ~/.claude/projects 下的 JSONL 会话，聚合为 Dashboard 指标。
// claudeDir 可留空（走 $HOME/.claude）；传非空路径时允许用户自定义 .claude 位置。
func (a *App) BuildClaudeDashboard(claudeDir string) (*claudeinsight.DashboardReport, error) {
	return claudeinsight.BuildDashboard(claudeDir)
}

// ListClaudeSessions 列出本机所有 Claude Code 会话（用于会话浏览页）。
// 返回按结束时间倒序排列。
func (a *App) ListClaudeSessions(claudeDir string) (*claudeinsight.SessionList, error) {
	return claudeinsight.ListSessions(claudeDir)
}

// LoadClaudeSession 读取单个会话的完整消息流（用于会话详情页）。
func (a *App) LoadClaudeSession(filePath string) (*claudeinsight.SessionDetail, error) {
	return claudeinsight.LoadSession(filePath)
}

// SearchClaudeSessions 跨所有会话的全文搜索（大小写无关）。
func (a *App) SearchClaudeSessions(query string) (*claudeinsight.SearchResult, error) {
	return claudeinsight.SearchSessions("", query, 0)
}

// PickClaudeExportPath 弹保存对话框,让用户选 ZIP 保存位置。
func (a *App) PickClaudeExportPath(defaultFilename string) (string, error) {
	return system.PickSaveFile(a.ctx, system.PickFileOptions{
		Title:           "导出会话到 ZIP",
		Extensions:      []string{".zip"},
		DisplayName:     "ZIP 压缩包",
		DefaultFilename: defaultFilename,
	})
}

// PickClaudeImportPath 弹打开对话框,让用户选要导入的 ZIP。
func (a *App) PickClaudeImportPath() (string, error) {
	return system.PickFile(a.ctx, system.PickFileOptions{
		Title:       "选择要导入的 ZIP",
		Extensions:  []string{".zip"},
		DisplayName: "ZIP 压缩包",
	})
}

// ExportClaudeSessions 把选定的会话文件打包成 ZIP。
func (a *App) ExportClaudeSessions(filePaths []string, destZip string) (*claudeinsight.ExportResult, error) {
	return claudeinsight.ExportSessions("", filePaths, destZip)
}

// ImportClaudeSessions 从 ZIP 恢复会话到 ~/.claude/projects/。
func (a *App) ImportClaudeSessions(zipPath string) (*claudeinsight.ImportResult, error) {
	return claudeinsight.ImportSessions("", zipPath)
}

// ListClaudeSkills 列出 ~/.claude/skills 下所有 skill。
func (a *App) ListClaudeSkills() (*claudeinsight.SkillList, error) {
	return claudeinsight.ListSkills("")
}

// ListClaudeSkillFiles 列出某个 skill 目录下的所有文件。
func (a *App) ListClaudeSkillFiles(skill string) (*claudeinsight.SkillFileList, error) {
	return claudeinsight.ListSkillFiles("", skill)
}

// ReadClaudeSkillFile 读取 skill 下一个文件的内容。
func (a *App) ReadClaudeSkillFile(skill, relPath string) (*claudeinsight.SkillFileContent, error) {
	return claudeinsight.ReadSkillFile("", skill, relPath)
}

// WriteClaudeSkillFile 写入 / 覆盖 skill 下一个文件。
func (a *App) WriteClaudeSkillFile(skill, relPath, content string) error {
	return claudeinsight.WriteSkillFile("", skill, relPath, content)
}

// CreateClaudeSkill 新建一个 skill 目录,并写入默认 SKILL.md 模板。
func (a *App) CreateClaudeSkill(name string) error {
	return claudeinsight.CreateSkill("", name)
}

// DeleteClaudeSkill 删除整个 skill 目录。
func (a *App) DeleteClaudeSkill(name string) error {
	return claudeinsight.DeleteSkill("", name)
}

// DeleteClaudeSkillFile 删除 skill 下某个文件或空目录。
func (a *App) DeleteClaudeSkillFile(skill, relPath string) error {
	return claudeinsight.DeleteSkillFile("", skill, relPath)
}

// ---- Claude 配置文件（settings.json / CLAUDE.md）----

func (a *App) ReadClaudeConfigFile(name string) (*claudeinsight.ConfigFile, error) {
	return claudeinsight.ReadConfigFile("", name)
}

func (a *App) WriteClaudeConfigFile(name, content string) error {
	return claudeinsight.WriteConfigFile("", name, content)
}

// ================ Codex Insight ================

// BuildCodexDashboard 扫描 ~/.codex/sessions 下的 JSONL，聚合 Dashboard 指标。
func (a *App) BuildCodexDashboard(codexDir string) (*codexinsight.DashboardReport, error) {
	return codexinsight.BuildDashboard(codexDir)
}

// ListCodexSessions 列出所有 Codex 会话（按结束时间倒序）。
func (a *App) ListCodexSessions(codexDir string) (*codexinsight.SessionList, error) {
	return codexinsight.ListSessions(codexDir)
}

// LoadCodexSession 读取单个会话的完整消息流。
func (a *App) LoadCodexSession(filePath string) (*codexinsight.SessionDetail, error) {
	return codexinsight.LoadSession(filePath)
}

// SearchCodexSessions 跨所有 Codex 会话全文搜索。
func (a *App) SearchCodexSessions(query string) (*codexinsight.SearchResult, error) {
	return codexinsight.SearchSessions("", query, 0)
}

// ---- Codex Bundle 导入导出 ----

func (a *App) PickCodexExportPath(defaultFilename string) (string, error) {
	return system.PickSaveFile(a.ctx, system.PickFileOptions{
		Title:           "导出 Codex 会话到 ZIP",
		Extensions:      []string{".zip"},
		DisplayName:     "ZIP 压缩包",
		DefaultFilename: defaultFilename,
	})
}

func (a *App) PickCodexImportPath() (string, error) {
	return system.PickFile(a.ctx, system.PickFileOptions{
		Title:       "选择要导入的 Codex ZIP",
		Extensions:  []string{".zip"},
		DisplayName: "ZIP 压缩包",
	})
}

func (a *App) ExportCodexSessions(filePaths []string, destZip string) (*codexinsight.ExportResult, error) {
	return codexinsight.ExportSessions("", filePaths, destZip)
}

func (a *App) ImportCodexSessions(zipPath string) (*codexinsight.ImportResult, error) {
	return codexinsight.ImportSessions("", zipPath)
}

// ---- Codex Memories ----

func (a *App) ListCodexMemories() (*codexinsight.MemoryFileList, error) {
	return codexinsight.ListMemories("")
}

func (a *App) ReadCodexMemory(relPath string) (*codexinsight.MemoryFileContent, error) {
	return codexinsight.ReadMemory("", relPath)
}

func (a *App) WriteCodexMemory(relPath, content string) error {
	return codexinsight.WriteMemory("", relPath, content)
}

func (a *App) DeleteCodexMemory(relPath string) error {
	return codexinsight.DeleteMemory("", relPath)
}

// ---- Codex 配置文件(AGENTS.md / config.toml) ----

func (a *App) ReadCodexConfigFile(name string) (*codexinsight.ConfigFile, error) {
	return codexinsight.ReadConfigFile("", name)
}

func (a *App) WriteCodexConfigFile(name, content string) error {
	return codexinsight.WriteConfigFile("", name, content)
}

// ---- Codex 全局历史 ----

func (a *App) ListCodexHistory(query string) (*codexinsight.HistoryResult, error) {
	return codexinsight.ListHistory("", query)
}

// ================ Updater ================

// CheckUpdate 对比 Hub manifest 与本地版本
func (a *App) CheckUpdate() (*updater.CheckResult, error) {
	return updater.Check(a.ctx, AppVersion)
}

// DownloadUpdate 下载 manifest 指向的新版到 Downloads,期间通过
// Wails 事件 "update:download-progress" 推送进度
func (a *App) DownloadUpdate(m updater.Manifest) (*updater.DownloadResult, error) {
	return updater.Download(a.ctx, a.ctx, m)
}

// QuitForUpdate 仅仅关闭 app(不启动新 exe)——
// 给用户"我先手动处理"的口子,通常不走这条。
func (a *App) QuitForUpdate() {
	wailsruntime.Quit(a.ctx)
}

// InstallAndRestart 一键安装:
//  1. 后台 detached 启动 Downloads 里的新 exe
//  2. 500ms 后关闭当前 app,让新 exe 完成自搬家流程
//
// 新 exe 里的 HandleStartup 有 4 次重试(总 ~3 秒),足够容忍我们这边的优雅退出。
func (a *App) InstallAndRestart(downloadedPath string) error {
	cmd := exec.Command(downloadedPath)
	cmd.SysProcAttr = detachedSysProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		wailsruntime.Quit(a.ctx)
	}()
	return nil
}

// OpenDownloadsFolder 方便用户找刚下载的 exe
func (a *App) OpenDownloadsFolder() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return system.OpenInExplorer(filepath.Join(home, "Downloads"))
}
