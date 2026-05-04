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
	"tool_forge/backend/tools/aichat"
	"tool_forge/backend/tools/aistupid"
	"tool_forge/backend/tools/appsearch"
	"tool_forge/backend/tools/charles"
	"tool_forge/backend/tools/claudeinsight"
	"tool_forge/backend/tools/clipboard"
	"tool_forge/backend/tools/codexinsight"
	"tool_forge/backend/tools/envscan"
	"tool_forge/backend/tools/forensic"
	"tool_forge/backend/tools/httptest"
	"tool_forge/backend/tools/netscan"
	"tool_forge/backend/tools/providerswitch"
	"tool_forge/backend/updater"
)

// AppVersion 应用版本号，随 wails.json 同步维护
const AppVersion = "0.1.9"

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
	ctx       context.Context
	forensic  *forensic.Service
	appsearch *appsearch.Service
	clipboard *clipboard.Service
	hotkey    *system.Manager
	httptest  *httptest.Service
	provider  *providerswitch.Service
	aichat    *aichat.Service
}

// NewApp creates a new App application struct
func NewApp() *App {
	clip, err := clipboard.New()
	if err != nil {
		// 数据目录创建失败时降级:服务为 nil,前端 RPC 会得到默认错误
		clip = nil
	}
	// 全局热键 manager:声明所有可绑定的 action,用户可在 Profile → 快捷键里自定义
	hkConfig := ""
	if home, err := os.UserHomeDir(); err == nil {
		hkConfig = filepath.Join(home, ".toolforge", "hotkeys.json")
	}
	hkManager := system.NewManager([]system.Action{
		{
			ID:          "clipboard.open",
			Label:       "打开剪贴板历史",
			Event:       "nav:goto-clipboard",
			DefaultSpec: "Ctrl+Shift+V",
		},
	}, hkConfig)
	htt, _ := httptest.New()
	prov, _ := providerswitch.New()
	aic, _ := aichat.New()
	return &App{
		forensic:  forensic.New(),
		appsearch: appsearch.New(),
		clipboard: clip,
		hotkey:    hkManager,
		httptest:  htt,
		provider:  prov,
		aichat:    aic,
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.forensic.SetContext(ctx)
	if a.clipboard != nil {
		a.clipboard.Start(ctx)
		a.clipboard.LogStartup()
	}
	// 启动全局热键 manager(从 ~/.toolforge/hotkeys.json 读用户配置)
	if a.hotkey != nil {
		a.hotkey.Start(ctx)
	}
	// AI 聊天 service 需要持有 wails ctx 才能 EventsEmit
	if a.aichat != nil {
		a.aichat.SetWailsContext(ctx)
	}
}

// shutdown 在 Wails 关闭前调用,释放剪贴板监听等
func (a *App) shutdown(ctx context.Context) {
	if a.clipboard != nil {
		a.clipboard.Stop()
	}
	if a.hotkey != nil {
		a.hotkey.Stop()
	}
}

// ================ Hotkey 管理(供 Profile → 快捷键 调用) ================

// ListHotkeys 返回所有可绑定的全局热键 + 当前状态
func (a *App) ListHotkeys() []system.HotkeyInfo {
	if a.hotkey == nil {
		return nil
	}
	return a.hotkey.List()
}

// SetHotkey 重新绑定某个 action;spec 为空表示取消绑定
func (a *App) SetHotkey(id, spec string) string {
	if a.hotkey == nil {
		return "热键 manager 未初始化"
	}
	if err := a.hotkey.Set(id, spec); err != nil {
		return err.Error()
	}
	return ""
}

// ResetHotkey 把某个 action 还原成默认
func (a *App) ResetHotkey(id string) string {
	if a.hotkey == nil {
		return "热键 manager 未初始化"
	}
	if err := a.hotkey.Reset(id); err != nil {
		return err.Error()
	}
	return ""
}

// ================ Data 管理(供 Profile → 数据 调用) ================

// GetDataStats 返回 ~/.toolforge 的概况
func (a *App) GetDataStats() *system.DataStats {
	s, _ := system.CollectDataStats()
	return s
}

// OpenDataDir 资源管理器打开 ~/.toolforge
func (a *App) OpenDataDir() string {
	if err := system.OpenDataDir(); err != nil {
		return err.Error()
	}
	return ""
}

// ClearAIDataModule 按 key 清空某个模块的数据(参见 system.DataStats.Modules)
//
//	注意:对应 service 仍在运行可能会立刻再写回,UI 提示需要重启 App 后再操作的模块
func (a *App) ClearAIDataModule(key string) string {
	// 先尝试停止与该 key 关联的 service,降低 race
	switch key {
	case "clipboard":
		if a.clipboard != nil {
			a.clipboard.Stop()
		}
	case "hotkeys":
		if a.hotkey != nil {
			a.hotkey.Stop()
		}
	}
	if err := system.ClearModuleData(key); err != nil {
		return err.Error()
	}
	return ""
}

// ExportData 把数据目录 + 前端传来的 localStorage 一起打 zip,
// localStorageJSON 是前端 stringify 后的 JSON 字符串。返回保存路径(空 = 用户取消)
func (a *App) ExportData(localStorageJSON string) (string, string) {
	path, err := system.ExportData(a.ctx, localStorageJSON)
	if err != nil {
		return "", err.Error()
	}
	return path, ""
}

// ImportData 选 zip 并恢复到 ~/.toolforge,返回需要前端写回的 localStorage JSON。
// 返回 (localStorageJSON, errorMsg);用户取消时两个都为空。
// 注意:调用前会先停止 clipboard service,导入后需要前端提示用户重启 App。
func (a *App) ImportData() (string, string) {
	if a.clipboard != nil {
		a.clipboard.Stop()
	}
	ls, err := system.ImportData(a.ctx)
	if err != nil {
		return "", err.Error()
	}
	return ls, ""
}

// ================ HTTP 请求测试器 ================

// SendHTTPRequest 发送一次 HTTP 请求并返回响应
func (a *App) SendHTTPRequest(req httptest.Request) httptest.Response {
	if a.httptest == nil {
		return httptest.Response{Error: "HTTP 服务未初始化"}
	}
	return a.httptest.Send(req)
}

// ListHTTPHistory 返回历史记录
func (a *App) ListHTTPHistory() []httptest.HistoryItem {
	if a.httptest == nil {
		return nil
	}
	return a.httptest.History()
}

// DeleteHTTPHistory 删除单条历史
func (a *App) DeleteHTTPHistory(id string) {
	if a.httptest == nil {
		return
	}
	a.httptest.DeleteHistory(id)
}

// ClearHTTPHistory 清空所有历史
func (a *App) ClearHTTPHistory() {
	if a.httptest == nil {
		return
	}
	a.httptest.ClearHistory()
}

// ================ Provider 切换器(Claude Code / Codex 多 API 配置) ================

// ListProviders 返回所有保存的 Provider(active 排在最前)
func (a *App) ListProviders() []providerswitch.Provider {
	if a.provider == nil {
		return nil
	}
	return a.provider.List()
}

// ListProviderPresets 返回内置预设(Anthropic / GLM / Kimi / OpenAI 等)
func (a *App) ListProviderPresets() []providerswitch.Preset {
	if a.provider == nil {
		return nil
	}
	return a.provider.ListPresets()
}

// SaveProvider 新增或更新一条;id 为空 → 新增,否则更新
func (a *App) SaveProvider(p providerswitch.Provider) (providerswitch.Provider, string) {
	if a.provider == nil {
		return providerswitch.Provider{}, "Provider 服务未初始化"
	}
	saved, err := a.provider.Save(p)
	if err != nil {
		return providerswitch.Provider{}, err.Error()
	}
	return saved, ""
}

// DeleteProvider 删除一条 Provider
func (a *App) DeleteProvider(id string) string {
	if a.provider == nil {
		return "Provider 服务未初始化"
	}
	if err := a.provider.Delete(id); err != nil {
		return err.Error()
	}
	return ""
}

// ActivateProvider 把指定 Provider 写入 ~/.claude/settings.json 或 ~/.codex/config.toml
func (a *App) ActivateProvider(id string) providerswitch.ApplyResult {
	if a.provider == nil {
		return providerswitch.ApplyResult{OK: false, Message: "Provider 服务未初始化"}
	}
	return a.provider.Activate(id)
}

// TestProvider 用指定配置发一个 max_tokens=1 的探测请求
func (a *App) TestProvider(p providerswitch.Provider) providerswitch.TestResult {
	if a.provider == nil {
		return providerswitch.TestResult{OK: false, Message: "Provider 服务未初始化"}
	}
	return a.provider.Test(p)
}

// GetActiveProviderConfig 读当前 ~/.claude 或 ~/.codex 实际生效的配置(用于 UI 对比)
func (a *App) GetActiveProviderConfig(t string) map[string]string {
	if a.provider == nil {
		return nil
	}
	return a.provider.GetActiveConfig(providerswitch.ProviderType(t))
}

// ================ 网络工具(SSL / DNS / WHOIS / 端口) ================

// CheckSSLCert 拉 host:port 的 TLS 证书链
func (a *App) CheckSSLCert(host string, port int) netscan.SSLResult {
	return netscan.CheckSSL(host, port, 8000)
}

// LookupDNSRecords 查 DNS 记录;types 为空时查 A/AAAA/CNAME/MX/TXT/NS
func (a *App) LookupDNSRecords(domain string, types []string) netscan.DNSResult {
	return netscan.LookupDNS(domain, types)
}

// LookupWhoisInfo 走 TCP 43 查 WHOIS
func (a *App) LookupWhoisInfo(domain string) netscan.WhoisResult {
	return netscan.LookupWhois(domain)
}

// ScanPorts 并发探测端口列表是否开放
func (a *App) ScanPorts(host string, ports []int, timeoutMs int) netscan.PortResult {
	return netscan.ScanPorts(host, ports, timeoutMs)
}

// ResetAllData 清空整个 ~/.toolforge 目录,前端在调用前应清自家 localStorage,
// 调用后需要提示用户重启
func (a *App) ResetAllData() string {
	if a.clipboard != nil {
		a.clipboard.Stop()
	}
	if a.hotkey != nil {
		a.hotkey.Stop()
	}
	if err := system.ResetAllData(); err != nil {
		return err.Error()
	}
	return ""
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

// ================ App Search ================

// SearchApp 多源并发搜索 app 包名/基本信息。
// 需要 PHPSESSID 的源（七麦 Android）在此从系统凭据库读取并注入。
func (a *App) SearchApp(req appsearch.SearchRequest) (*appsearch.SearchResponse, error) {
	if needsQimaiPhpSessID(req.Sources) {
		if sid, err := system.GetPassword(appsearch.KeyringQimaiPhpSessID); err == nil && sid != "" {
			req.SetQimaiPhpSessID(sid)
		}
	}
	return a.appsearch.Search(a.ctx, req)
}

func needsQimaiPhpSessID(sources []appsearch.SourceID) bool {
	for _, s := range sources {
		if s == appsearch.SourceQimaiAndroid {
			return true
		}
	}
	return false
}

// HasQimaiPhpSessID 供 Profile 页判断凭据库里是否已保存 PHPSESSID（不泄露值）。
func (a *App) HasQimaiPhpSessID() bool {
	v, err := system.GetPassword(appsearch.KeyringQimaiPhpSessID)
	return err == nil && v != ""
}

// SaveQimaiPhpSessID 保存 PHPSESSID 到系统凭据库；空字符串等价于删除。
func (a *App) SaveQimaiPhpSessID(value string) error {
	if value == "" {
		return system.DeletePassword(appsearch.KeyringQimaiPhpSessID)
	}
	return system.SavePassword(appsearch.KeyringQimaiPhpSessID, value)
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

// FetchAIStupidLeaderboard 拉取 aistupidlevel.info 的排行榜（含 7 日历史走势）。
func (a *App) FetchAIStupidLeaderboard() (*aistupid.LeaderboardResponse, error) {
	return aistupid.FetchLeaderboard(a.ctx)
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

// DeleteClaudeSession 删除单个 Claude 会话文件。
func (a *App) DeleteClaudeSession(filePath string) error {
	return claudeinsight.DeleteSession("", filePath)
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

// DeleteCodexSession 删除单个 Codex 会话文件。
func (a *App) DeleteCodexSession(filePath string) error {
	return codexinsight.DeleteSession("", filePath)
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

// ================ Clipboard ================

// ListClipboard 返回剪贴板历史 + 配置（启用状态/上限）
func (a *App) ListClipboard() clipboard.ListResult {
	if a.clipboard == nil {
		return clipboard.ListResult{Enabled: false, Limit: clipboard.DefaultConfig().Limit}
	}
	return a.clipboard.List()
}

// DeleteClipboardItem 删除单条
func (a *App) DeleteClipboardItem(id string) error {
	if a.clipboard == nil {
		return nil
	}
	return a.clipboard.Delete(id)
}

// ToggleClipboardPin 切换某条的置顶状态
func (a *App) ToggleClipboardPin(id string) error {
	if a.clipboard == nil {
		return nil
	}
	return a.clipboard.TogglePin(id)
}

// ClearClipboardHistory 清空非置顶历史
func (a *App) ClearClipboardHistory() error {
	if a.clipboard == nil {
		return nil
	}
	return a.clipboard.Clear()
}

// ClearClipboardAll 清空所有历史（含置顶）
func (a *App) ClearClipboardAll() error {
	if a.clipboard == nil {
		return nil
	}
	return a.clipboard.ClearAll()
}

// SetClipboardEnabled 启停剪贴板监听
func (a *App) SetClipboardEnabled(enabled bool) error {
	if a.clipboard == nil {
		return nil
	}
	return a.clipboard.SetEnabled(enabled)
}

// SetClipboardLimit 设置历史上限（同时按新上限裁剪）
func (a *App) SetClipboardLimit(limit int) error {
	if a.clipboard == nil {
		return nil
	}
	return a.clipboard.SetLimit(limit)
}

// SetClipboardMaxImageBytes 设置单张图片大小上限（字节）
func (a *App) SetClipboardMaxImageBytes(n int) error {
	if a.clipboard == nil {
		return nil
	}
	return a.clipboard.SetMaxImageBytes(n)
}

// CopyClipboardItem 把某条历史写回系统剪贴板
func (a *App) CopyClipboardItem(id string) error {
	if a.clipboard == nil {
		return nil
	}
	return a.clipboard.CopyItem(id)
}

// GetClipboardImage 读取某图片项的原图 dataURL（用于查看大图）
func (a *App) GetClipboardImage(id string) (string, error) {
	if a.clipboard == nil {
		return "", nil
	}
	return a.clipboard.GetImage(id)
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

// ================ AI 问答(OpenAI 兼容供应商) ================

// ListAIProviders 列出所有 AI 供应商(按 UpdatedAt 倒序)
func (a *App) ListAIProviders() []aichat.Provider {
	if a.aichat == nil {
		return nil
	}
	list, _ := a.aichat.ListProviders()
	return list
}

// SaveAIProvider 新增或更新供应商;ID 为空 → 新增
func (a *App) SaveAIProvider(p aichat.Provider) (aichat.Provider, string) {
	if a.aichat == nil {
		return aichat.Provider{}, "AI 服务未初始化"
	}
	saved, err := a.aichat.SaveProvider(p)
	if err != nil {
		return aichat.Provider{}, err.Error()
	}
	return saved, ""
}

// DeleteAIProvider 删除一条供应商
func (a *App) DeleteAIProvider(id string) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.DeleteProvider(id); err != nil {
		return err.Error()
	}
	return ""
}

// ToggleAIProvider 启用/禁用一条供应商
func (a *App) ToggleAIProvider(id string, enabled bool) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.ToggleProvider(id, enabled); err != nil {
		return err.Error()
	}
	return ""
}

// FetchAIModels 拉某个供应商的 /v1/models
func (a *App) FetchAIModels(providerID string) aichat.FetchModelsResult {
	if a.aichat == nil {
		return aichat.FetchModelsResult{OK: false, Message: "AI 服务未初始化"}
	}
	return a.aichat.FetchModels(providerID)
}

// TestAIProviderModel 用某个模型发一个 stream 探测请求(收到首 chunk 即成功)
func (a *App) TestAIProviderModel(providerID, modelID string) aichat.TestResult {
	if a.aichat == nil {
		return aichat.TestResult{OK: false, Message: "AI 服务未初始化"}
	}
	return a.aichat.TestProviderModel(providerID, modelID)
}

// GetAIConfig 默认助手模型
func (a *App) GetAIConfig() aichat.Config {
	if a.aichat == nil {
		return aichat.Config{}
	}
	c, _ := a.aichat.GetConfig()
	return c
}

// SaveAIConfig 保存默认助手模型
func (a *App) SaveAIConfig(c aichat.Config) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.SaveConfig(c); err != nil {
		return err.Error()
	}
	return ""
}

// ListAIUsage 返回 ~/.toolforge/ai-chat/usage.jsonl 全量记录(按时间正序)
func (a *App) ListAIUsage() []aichat.UsageRecord {
	if a.aichat == nil {
		return nil
	}
	list, _ := a.aichat.ListUsage()
	return list
}

// ListAIConversations 列出所有会话(按 UpdatedAt 倒序)
func (a *App) ListAIConversations() []aichat.ConversationSummary {
	if a.aichat == nil {
		return nil
	}
	list, _ := a.aichat.ListConversations()
	return list
}

// GetAIConversation 取一条会话(含全部消息)
func (a *App) GetAIConversation(id string) (aichat.Conversation, string) {
	if a.aichat == nil {
		return aichat.Conversation{}, "AI 服务未初始化"
	}
	c, err := a.aichat.GetConversation(id)
	if err != nil {
		return aichat.Conversation{}, err.Error()
	}
	return *c, ""
}

// CreateAIConversation 新建会话;providerID + modelID 决定本会话使用的模型;
// system 系统提示词(可空);contextCount 上下文条数(0 = 不限)
func (a *App) CreateAIConversation(providerID, modelID, title, system string, contextCount int) (aichat.Conversation, string) {
	if a.aichat == nil {
		return aichat.Conversation{}, "AI 服务未初始化"
	}
	c, err := a.aichat.CreateConversation(providerID, modelID, title, system, contextCount)
	if err != nil {
		return aichat.Conversation{}, err.Error()
	}
	return *c, ""
}

// UpdateAIConversationMeta 一次性更新会话标题/系统提示/上下文条数
func (a *App) UpdateAIConversationMeta(id, title, system string, contextCount int) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.UpdateConversationMeta(id, title, system, contextCount); err != nil {
		return err.Error()
	}
	return ""
}

// UpdateAIConversationContext 更新上下文条数(0 = 不限)
func (a *App) UpdateAIConversationContext(id string, count int) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.UpdateConversationContext(id, count); err != nil {
		return err.Error()
	}
	return ""
}

// InsertAIClearMarker 在会话末尾插入"清除上下文"分隔标记
func (a *App) InsertAIClearMarker(id string) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.InsertClearMarker(id); err != nil {
		return err.Error()
	}
	return ""
}

// UpdateAIConversationModel 切换会话的供应商 / 模型
func (a *App) UpdateAIConversationModel(id, providerID, modelID string) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.UpdateConversationModel(id, providerID, modelID); err != nil {
		return err.Error()
	}
	return ""
}

// RenameAIConversation 重命名会话
func (a *App) RenameAIConversation(id, title string) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.RenameConversation(id, title); err != nil {
		return err.Error()
	}
	return ""
}

// DeleteAIConversation 删除一条会话(同时取消进行中的流)
func (a *App) DeleteAIConversation(id string) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.DeleteConversationByID(id); err != nil {
		return err.Error()
	}
	return ""
}

// SendAIChat 在会话里追加 user 消息并启动流式回复
//
//	userImages 可选;每个 ImageBlock 用 base64(无 data: 前缀) 或 url 二选一
//	返回的 Conversation 已含 user 消息 + 空 assistant 占位;
//	后续每个 chunk 通过事件 ai-chat:chunk:{id} / done:{id} / error:{id} 推送
func (a *App) SendAIChat(convID, userContent string, userImages []aichat.ImageBlock) (aichat.Conversation, string) {
	if a.aichat == nil {
		return aichat.Conversation{}, "AI 服务未初始化"
	}
	c, err := a.aichat.SendChat(a.ctx, convID, userContent, userImages)
	if err != nil {
		return aichat.Conversation{}, err.Error()
	}
	return *c, ""
}

// UpdateAIConversationSystem 更新会话的系统提示词(空字符串=清除)
func (a *App) UpdateAIConversationSystem(id, system string) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.UpdateConversationSystem(id, system); err != nil {
		return err.Error()
	}
	return ""
}

// DeleteAIChatMessage 删除会话里的单条消息(user/assistant/clear 均可删)
func (a *App) DeleteAIChatMessage(convID, msgID string) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if err := a.aichat.DeleteMessage(convID, msgID); err != nil {
		return err.Error()
	}
	return ""
}

// EditAndResendAIChat 编辑某条 user 消息并重新发起流(截断该消息之后的所有内容)
func (a *App) EditAndResendAIChat(convID, msgID, newContent string) (aichat.Conversation, string) {
	if a.aichat == nil {
		return aichat.Conversation{}, "AI 服务未初始化"
	}
	c, err := a.aichat.EditAndResend(a.ctx, convID, msgID, newContent)
	if err != nil {
		return aichat.Conversation{}, err.Error()
	}
	return *c, ""
}

// RegenerateAILastChat 重新生成最后一条助手回复
//
//	要求会话最后一条是 assistant、前一条是 user;复用同一 message ID,前端原地刷新
func (a *App) RegenerateAILastChat(convID string) (aichat.Conversation, string) {
	if a.aichat == nil {
		return aichat.Conversation{}, "AI 服务未初始化"
	}
	c, err := a.aichat.RegenerateLast(a.ctx, convID)
	if err != nil {
		return aichat.Conversation{}, err.Error()
	}
	return *c, ""
}

// StopAIChat 取消正在进行的流;若该会话没有正在进行的流则返回错误
func (a *App) StopAIChat(convID string) string {
	if a.aichat == nil {
		return "AI 服务未初始化"
	}
	if !a.aichat.CancelStream(convID) {
		return "该会话没有进行中的请求"
	}
	return ""
}
