package aiconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

var tf = HTTPMCPServer{Name: "tool-forge", URL: "http://127.0.0.1:11435/mcp"}

// ---------- Claude Code ----------

// .claude.json 里几十个键都是 Claude Code 自己的:大整数、浮点、转义、缩进、键的顺序。
// 加一段 mcpServers 之后,这些必须一个字节都没变 —— 整个「解析再序列化」会把
// 大整数改写成浮点、把键按字母重排,用户以为只是加了一段
const claudeFixture = `{
  "numStartups": 42,
  "firstStartTime": "2026-01-02T03:04:05.678Z",
  "lastCost": 0.1234567890123,
  "bigNumber": 12345678901234567890,
  "projects": {
    "D:\\my_project\\x": {
      "allowedTools": [],
      "history": [
        {
          "display": "中文 \u003c 转义 \\ 反斜杠",
          "pastedContents": {}
        }
      ]
    }
  },
  "tipsHistory": {
    "new-user-warmup": 1
  }
}
`

func TestClaudeAddsMCPServersWithoutTouchingTheRest(t *testing.T) {
	sp, err := spliceClaudeJSON([]byte(claudeFixture), tf)
	if err != nil {
		t.Fatal(err)
	}
	if sp.action != "add" {
		t.Fatalf("action = %q, want add", sp.action)
	}
	out := string(sp.out)
	// 插在开括号后面,原来的内容原样跟在后面
	if !strings.HasSuffix(out, claudeFixture[1:]) {
		t.Fatalf("原有内容被改动了:\n%s", out)
	}
	wantHead := "{\n  \"mcpServers\": {\n    \"tool-forge\": {\n      \"type\": \"http\",\n      \"url\": \"http://127.0.0.1:11435/mcp\"\n    }\n  },\n"
	if !strings.HasPrefix(out, wantHead) {
		t.Fatalf("插入的段落格式不对:\n%s", out[:min(len(out), 240)])
	}
	if !strings.Contains(out, "12345678901234567890") {
		t.Fatal("大整数被改写了")
	}
	if !json.Valid(sp.out) {
		t.Fatal("拼出来的不是合法 JSON")
	}
}

func TestClaudeAppendsAfterExistingServersInOrder(t *testing.T) {
	src := `{
  "a": 1,
  "mcpServers": {
    "zeta": {
      "command": "node",
      "args": ["x.js"],
      "env": {"BIG": 99999999999999999}
    },
    "acemcp": {"type": "http", "url": "http://x/"}
  },
  "z": {"nested": [1, 2, 3]}
}
`
	sp, err := spliceClaudeJSON([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	if sp.action != "add" {
		t.Fatalf("action = %q", sp.action)
	}
	out := string(sp.out)
	// mcpServers 之外的字节一个不动
	loc, err := locateTopLevel([]byte(src), "mcpServers")
	if err != nil || !loc.found {
		t.Fatalf("定位失败: %v %+v", err, loc)
	}
	if !strings.HasPrefix(out, src[:loc.start]) || !strings.HasSuffix(out, src[loc.end:]) {
		t.Fatalf("mcpServers 之外的内容被改了:\n%s", out)
	}
	// 顺序保持:zeta、acemcp、然后才是我们
	iz, ia, it := strings.Index(out, `"zeta"`), strings.Index(out, `"acemcp"`), strings.Index(out, `"tool-forge"`)
	if !(iz < ia && ia < it) {
		t.Fatalf("顺序乱了: zeta=%d acemcp=%d tool-forge=%d", iz, ia, it)
	}
	if !strings.Contains(out, "99999999999999999") {
		t.Fatal("别的 server 里的大整数被改写了")
	}
	got := claudeServers(t, sp.out)
	if got["zeta"]["command"] != "node" || got["tool-forge"]["url"] != tf.URL {
		t.Fatalf("内容不对: %+v", got)
	}
}

func TestClaudeReplacesInPlaceKeepingUserKeys(t *testing.T) {
	src := `{
  "mcpServers": {
    "tool-forge": {
      "type": "http",
      "url": "http://127.0.0.1:9999/mcp",
      "headers": {"Authorization": "Bearer old"},
      "timeout": 30
    },
    "other": {"command": "x"}
  }
}
`
	sp, err := spliceClaudeJSON([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	if sp.action != "replace" {
		t.Fatalf("action = %q", sp.action)
	}
	if !strings.Contains(sp.oldBlock, "9999") {
		t.Fatalf("oldBlock 没带原来的内容: %s", sp.oldBlock)
	}
	got := claudeServers(t, sp.out)
	me := got["tool-forge"]
	if me["url"] != tf.URL {
		t.Fatalf("url 没换: %v", me["url"])
	}
	if _, has := me["headers"]; has {
		t.Fatal("没开鉴权,旧的 headers 该去掉")
	}
	if me["timeout"] != float64(30) {
		t.Fatal("用户自己加的 timeout 丢了")
	}
	if _, has := got["other"]; !has {
		t.Fatal("别的 server 丢了")
	}
	if strings.Index(string(sp.out), `"tool-forge"`) > strings.Index(string(sp.out), `"other"`) {
		t.Fatal("替换后位置变了,应该原位")
	}
}

func TestClaudeConvertsStdioEntryToHTTP(t *testing.T) {
	src := `{"mcpServers": {"tool-forge": {"command": "node", "args": ["a"], "env": {"X": "1"}}}}`
	sp, err := spliceClaudeJSON([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	me := claudeServers(t, sp.out)["tool-forge"]
	for _, k := range []string{"command", "args", "env"} {
		if _, has := me[k]; has {
			t.Errorf("换成 http 之后 %s 还在", k)
		}
	}
	if me["type"] != "http" {
		t.Error("type 不是 http")
	}
}

func TestClaudeUnchangedWhenAlreadyThere(t *testing.T) {
	src := `{"x": 1, "mcpServers": {"tool-forge": {"url": "http://127.0.0.1:11435/mcp", "type": "http"}}}`
	sp, err := spliceClaudeJSON([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	if sp.action != "unchanged" {
		t.Fatalf("action = %q", sp.action)
	}
	if string(sp.out) != src {
		t.Fatal("unchanged 还改了内容")
	}
}

func TestClaudeTokenGoesIntoHeaders(t *testing.T) {
	withToken := tf
	withToken.Token = `ab"c\d`
	sp, err := spliceClaudeJSON([]byte("{}"), withToken)
	if err != nil {
		t.Fatal(err)
	}
	hdrs, _ := claudeServers(t, sp.out)["tool-forge"]["headers"].(map[string]any)
	if hdrs["Authorization"] != `Bearer ab"c\d` {
		t.Fatalf("headers 不对: %v", hdrs)
	}
}

func TestClaudeRefusesNonObjectOrBroken(t *testing.T) {
	for _, src := range []string{`[]`, `{"a": }`, `"str"`} {
		if _, err := spliceClaudeJSON([]byte(src), tf); err == nil {
			t.Errorf("%q 应该报错", src)
		}
	}
}

func TestClaudeEmptyObjectAndOtherIndent(t *testing.T) {
	sp, err := spliceClaudeJSON([]byte("{}\n"), tf)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(sp.out) || !strings.HasSuffix(string(sp.out), "}\n") {
		t.Fatalf("空对象插入后不对:\n%s", sp.out)
	}
	// 四格缩进的文件跟着用四格
	sp, err = spliceClaudeJSON([]byte("{\n    \"a\": 1\n}"), tf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sp.out), "\n    \"mcpServers\": {\n        \"tool-forge\": {\n            \"type\"") {
		t.Fatalf("没有跟着文件的缩进走:\n%s", sp.out)
	}
}

func claudeServers(t *testing.T, out []byte) map[string]map[string]any {
	t.Helper()
	var parsed struct {
		MCP map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("改完解析不了: %v\n%s", err, out)
	}
	return parsed.MCP
}

// ---------- Codex ----------

const codexFixture = "# 我的 Codex 配置\nmodel = \"gpt-5\"\napproval_policy = \"never\"\n\n[mcp_servers.node_repl]\ncommand = \"node\"\nargs = [\"repl.js\"]\n\n[mcp_servers.node_repl.env]\nNODE_ENV = \"production\"\n\n# 下面是 profile\n[profiles.fast]\nmodel = \"gpt-5-mini\"\n"

func TestCodexAppendsBlockKeepingFileByteForByte(t *testing.T) {
	sp, err := spliceCodexTOML([]byte(codexFixture), tf)
	if err != nil {
		t.Fatal(err)
	}
	if sp.action != "add" {
		t.Fatalf("action = %q", sp.action)
	}
	out := string(sp.out)
	if !strings.HasPrefix(out, codexFixture) {
		t.Fatalf("原有内容被改动了:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n[mcp_servers.tool-forge]\nurl = \"http://127.0.0.1:11435/mcp\"\n") {
		t.Fatalf("追加的段不对:\n%s", out)
	}
	assertCodexParses(t, sp.out, tf, 2)
}

// 旧段里有多行数组:一行一行猜不准,整段换;但段外的注释和表一个字节不动
func TestCodexReplacesOwnBlockOnly(t *testing.T) {
	src := "model = \"gpt-5\"\n\n[mcp_servers.tool-forge]\ncommand = \"old\"\nargs = [\n  \"a\",\n]\n# 这条是用户加的\nstartup_timeout_sec = 30\n\n# 给 profile 的注释\n[profiles.fast]\nmodel = \"x\"\n"
	sp, err := spliceCodexTOML([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	if sp.action != "replace" {
		t.Fatalf("action = %q", sp.action)
	}
	out := string(sp.out)
	if !strings.HasPrefix(out, "model = \"gpt-5\"\n\n[mcp_servers.tool-forge]\nurl = \"http://127.0.0.1:11435/mcp\"\n") {
		t.Fatalf("开头不对:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n\n# 给 profile 的注释\n[profiles.fast]\nmodel = \"x\"\n") {
		t.Fatalf("段后面的内容被动了:\n%s", out)
	}
	if strings.Contains(out, "old") || strings.Contains(out, "startup_timeout_sec") {
		t.Fatalf("多行值的旧段应整段换掉:\n%s", out)
	}
	if !strings.Contains(sp.oldBlock, "command = \"old\"") {
		t.Fatalf("oldBlock 不完整: %s", sp.oldBlock)
	}
	assertCodexParses(t, sp.out, tf, 1)
}

// 旧段每行都认得出:我们管的键换掉,用户自己加的键和注释原位留着
func TestCodexReplaceKeepsUserKeysWhenSimple(t *testing.T) {
	src := "[mcp_servers.tool-forge]\nurl = \"http://127.0.0.1:9999/mcp\"\nbearer_token_env_var = \"OLD\"\n# 用户自己加的\nstartup_timeout_sec = 30\nenabled = true\n"
	sp, err := spliceCodexTOML([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	want := "[mcp_servers.tool-forge]\nurl = \"http://127.0.0.1:11435/mcp\"\n# 用户自己加的\nstartup_timeout_sec = 30\nenabled = true\n"
	if string(sp.out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", sp.out, want)
	}
	if sp.newBlock != strings.TrimSuffix(want, "\n") {
		t.Fatalf("预览的段和真写的不一样:\n%s", sp.newBlock)
	}
	assertCodexParses(t, sp.out, tf, 1)
}

func TestCodexSubTableForcesWholeReplace(t *testing.T) {
	src := "[mcp_servers.tool-forge]\ncommand = \"x\"\n\n[mcp_servers.tool-forge.env]\nA = \"1\"\n\n[other]\nk = 1\n"
	sp, err := spliceCodexTOML([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	want := "[mcp_servers.tool-forge]\nurl = \"http://127.0.0.1:11435/mcp\"\n\n[other]\nk = 1\n"
	if string(sp.out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", sp.out, want)
	}
	assertCodexParses(t, sp.out, tf, 1)
}

// 子表被人放到了别处:也得一起清掉,否则换成 http 之后还挂着一个 env 子表
func TestCodexScatteredSubTableRemoved(t *testing.T) {
	src := "[mcp_servers.tool-forge]\ncommand = \"x\"\n\n[other]\nk = 1\n\n[mcp_servers.tool-forge.env]\nA = \"1\"\n"
	sp, err := spliceCodexTOML([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	out := string(sp.out)
	if !strings.HasPrefix(out, "[mcp_servers.tool-forge]\nurl = \"http://127.0.0.1:11435/mcp\"\n\n[other]\nk = 1\n") {
		t.Fatalf("got:\n%s", out)
	}
	if strings.Contains(out, "A = ") || strings.Contains(out, ".env]") {
		t.Fatalf("散落的子表没清掉:\n%s", out)
	}
	assertCodexParses(t, sp.out, tf, 1)
}

func TestCodexUnchangedIgnoresUserKeys(t *testing.T) {
	src := "[mcp_servers.tool-forge]\nurl = \"http://127.0.0.1:11435/mcp\"\nstartup_timeout_sec = 30\n"
	sp, err := spliceCodexTOML([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	if sp.action != "unchanged" {
		t.Fatalf("action = %q", sp.action)
	}
	if string(sp.out) != src {
		t.Fatal("unchanged 还改了内容")
	}

	// 鉴权变了就不算一样
	withToken := tf
	withToken.Token = "t0k"
	sp, err = spliceCodexTOML([]byte(src), withToken)
	if err != nil {
		t.Fatal(err)
	}
	if sp.action != "replace" {
		t.Fatalf("action = %q", sp.action)
	}
	if !strings.Contains(string(sp.out), "http_headers = { Authorization = \"Bearer t0k\" }") {
		t.Fatalf("没写 http_headers:\n%s", sp.out)
	}
	if !strings.Contains(string(sp.out), "startup_timeout_sec = 30") {
		t.Fatalf("用户的键丢了:\n%s", sp.out)
	}
	assertCodexParses(t, sp.out, withToken, 1)
}

func TestCodexRefusesInlineTableForm(t *testing.T) {
	src := "[mcp_servers]\ntool-forge = { url = \"http://x/\" }\n"
	_, err := spliceCodexTOML([]byte(src), tf)
	if err == nil || !strings.Contains(err.Error(), "别的写法") {
		t.Fatalf("应该拒绝并说明,得到: %v", err)
	}
}

func TestCodexRefusesBrokenFile(t *testing.T) {
	if _, err := spliceCodexTOML([]byte("model = \n[[[\n"), tf); err == nil {
		t.Fatal("坏文件应该拒绝")
	}
}

func TestCodexKeepsCRLFAndQuotedHeader(t *testing.T) {
	src := "model = \"a\"\r\n\r\n[mcp_servers.\"tool-forge\"]\r\ncommand = \"old\"\r\n\r\n[x]\r\ny = 1\r\n"
	sp, err := spliceCodexTOML([]byte(src), tf)
	if err != nil {
		t.Fatal(err)
	}
	if sp.action != "replace" {
		t.Fatalf("带引号的表头没认出来: %q", sp.action)
	}
	want := "model = \"a\"\r\n\r\n[mcp_servers.tool-forge]\r\nurl = \"http://127.0.0.1:11435/mcp\"\r\n\r\n[x]\r\ny = 1\r\n"
	if string(sp.out) != want {
		t.Fatalf("got:\n%q\nwant:\n%q", sp.out, want)
	}
	assertCodexParses(t, sp.out, tf, 1)
}

func TestCodexEmptyFileAndTokenEscaping(t *testing.T) {
	withToken := tf
	withToken.Token = `q"uote`
	sp, err := spliceCodexTOML(nil, withToken)
	if err != nil {
		t.Fatal(err)
	}
	want := "[mcp_servers.tool-forge]\nurl = \"http://127.0.0.1:11435/mcp\"\nhttp_headers = { Authorization = \"Bearer q\\\"uote\" }\n"
	if string(sp.out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", sp.out, want)
	}
	assertCodexParses(t, sp.out, withToken, 1)
}

func assertCodexParses(t *testing.T, out []byte, srv HTTPMCPServer, wantServers int) {
	t.Helper()
	var parsed map[string]any
	if _, err := toml.Decode(string(out), &parsed); err != nil {
		t.Fatalf("改完解析不了: %v\n%s", err, out)
	}
	ms, _ := parsed["mcp_servers"].(map[string]any)
	if len(ms) != wantServers {
		t.Fatalf("mcp_servers 数量 = %d, want %d\n%s", len(ms), wantServers, out)
	}
	m, _ := ms[srv.Name].(map[string]any)
	if !codexMatches(m, srv) {
		t.Fatalf("我们那段没读对: %+v", m)
	}
}

// ---------- 落盘 ----------

func TestInstallMCPWritesWithBackupAndDryRunDoesNot(t *testing.T) {
	home := t.TempDir()
	h := Home{Dir: home}
	file := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(codexFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	// 试算:不落盘
	res, err := InstallMCP(h, MCPTargetCodex, tf, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.Action != "add" || res.Backup != "" {
		t.Fatalf("试算不该落盘: %+v", res)
	}
	if got, _ := os.ReadFile(file); string(got) != codexFixture {
		t.Fatal("试算改了文件")
	}

	res, err = InstallMCP(h, MCPTargetCodex, tf, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || res.Backup == "" {
		t.Fatalf("真写入应该落盘并留备份: %+v", res)
	}
	if bak, _ := os.ReadFile(res.Backup); string(bak) != codexFixture {
		t.Fatal("备份内容不是原文件")
	}
	got, _ := os.ReadFile(file)
	if !strings.HasPrefix(string(got), codexFixture) || !strings.Contains(string(got), "[mcp_servers.tool-forge]") {
		t.Fatalf("写入结果不对:\n%s", got)
	}
	if _, err := os.Stat(file + ".tmp"); err == nil {
		t.Fatal("临时文件没清理")
	}

	// 再来一次:已经是这份了,不写、不备份
	entries, _ := os.ReadDir(filepath.Dir(file))
	before := len(entries)
	res, err = InstallMCP(h, MCPTargetCodex, tf, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.Action != "unchanged" {
		t.Fatalf("第二次应该是 unchanged: %+v", res)
	}
	if entries, _ = os.ReadDir(filepath.Dir(file)); len(entries) != before {
		t.Fatal("unchanged 还留了备份")
	}
}

func TestInstallMCPCreatesMissingClaudeJSON(t *testing.T) {
	home := t.TempDir()
	res, err := InstallMCP(Home{Dir: home}, MCPTargetClaude, tf, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "create" || !res.Applied || res.Backup != "" {
		t.Fatalf("新建的情况: %+v", res)
	}
	got, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	if claudeServers(t, got)["tool-forge"]["url"] != tf.URL {
		t.Fatalf("内容不对: %s", got)
	}
}

func TestInstallMCPRejectsBadNameAndTarget(t *testing.T) {
	h := Home{Dir: t.TempDir()}
	if _, err := InstallMCP(h, MCPTargetCodex, HTTPMCPServer{Name: "a.b", URL: "http://x"}, false); err == nil {
		t.Error("带点的名字要拒绝")
	}
	if _, err := InstallMCP(h, "cursor", tf, false); err == nil {
		t.Error("不认识的目标要拒绝")
	}
}
