package aiconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// 把工具箱自己的 MCP 端点写进 Claude Code / Codex 的配置。
//
// 两个文件都是别家程序在维护的:.claude.json 里塞着几十个键的统计和缓存,
// config.toml 是手写的、带注释。所以这里不走「解析 → 改 → 整个再序列化」——
// 那会把注释抹掉、把键重排、把不认识的字段丢掉,而用户以为只是加了一段。
// JSON 只替换顶层 mcpServers 这一个值所占的字节区间,TOML 只替换
// [mcp_servers.<名字>] 这一段行;其余内容一个字节不动。落盘前留带时间戳的 .bak。
//
// 已经配过的情况是常态(用户手抄过一遍),所以同名条目就地更新,绝不追加第二份;
// 内容已经一样就什么都不做,连备份都不留。

const (
	MCPTargetClaude = "claude"
	MCPTargetCodex  = "codex"
)

// HTTPMCPServer 要写进去的一条 http 传输的 MCP server
type HTTPMCPServer struct {
	Name string
	URL  string
	// Token 空 = 不带 Authorization 头
	Token string
}

// MCPInstallResult 试算或写入的结果
type MCPInstallResult struct {
	Target string `json:"target"`
	File   string `json:"file"`
	// Action 会做什么:
	//   create    文件不存在,新建
	//   add       追加一段
	//   replace   换掉已有的那段
	//   unchanged 已经是这份,不动
	Action string `json:"action"`
	// Block 将写入的那一段,给确认框看
	Block string `json:"block"`
	// OldBlock replace 时原来那一段
	OldBlock string `json:"oldBlock,omitempty"`
	// Backup 真写入后备份文件的路径
	Backup string `json:"backup,omitempty"`
	// Applied 这次是不是真的落盘了
	Applied bool `json:"applied"`
}

// spliced 一次拼接的结果;action 与 MCPInstallResult.Action 同
type spliced struct {
	out      []byte
	action   string
	oldBlock string
	newBlock string
}

var mcpNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// InstallMCP 把 srv 写进 target 的配置。apply 为假只试算不落盘。
func InstallMCP(h Home, target string, srv HTTPMCPServer, apply bool) (*MCPInstallResult, error) {
	// 名字同时要当 TOML 的裸键和 JSON 的键,限死在这几种字符里,
	// 省得在两种语法里各转义一遍
	if !mcpNameRe.MatchString(srv.Name) {
		return nil, fmt.Errorf("MCP 名字只能用字母、数字、下划线和短横线: %q", srv.Name)
	}
	if strings.TrimSpace(srv.URL) == "" {
		return nil, errors.New("MCP 地址为空")
	}
	home, err := h.resolve()
	if err != nil {
		return nil, err
	}
	var (
		file   string
		splice func([]byte, HTTPMCPServer) (*spliced, error)
	)
	switch target {
	case MCPTargetClaude:
		file = filepath.Join(home, ".claude.json")
		splice = spliceClaudeJSON
	case MCPTargetCodex:
		file = filepath.Join(home, ".codex", "config.toml")
		splice = spliceCodexTOML
	default:
		return nil, fmt.Errorf("不认识的写入目标: %q", target)
	}

	data, err := os.ReadFile(file)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("%s 太大(%d 字节),不敢动", file, len(data))
	}
	sp, err := splice(data, srv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	res := &MCPInstallResult{
		Target:   target,
		File:     file,
		Action:   sp.action,
		Block:    sp.newBlock,
		OldBlock: sp.oldBlock,
	}
	if !exists {
		res.Action = "create"
	}
	if res.Action == "unchanged" || !apply {
		return res, nil
	}

	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return nil, err
	}
	mode := fs.FileMode(0o644)
	if exists {
		if info, err := os.Stat(file); err == nil {
			mode = info.Mode().Perm()
		}
		bak, err := backupFile(file)
		if err != nil {
			return nil, err
		}
		res.Backup = bak
	}
	if err := writeAtomic(file, sp.out, mode); err != nil {
		return nil, err
	}
	res.Applied = true
	return res, nil
}

// writeAtomic 先写临时文件再改名,写到一半断电也不会留下半个配置
func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ---------- Claude Code:~/.claude.json ----------

// jsonKV 对象里的一对键值,值保留原始字节
type jsonKV struct {
	key string
	raw json.RawMessage
}

func spliceClaudeJSON(data []byte, srv HTTPMCPServer) (*spliced, error) {
	indent := detectJSONIndent(data)
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	if !json.Valid(data) {
		return nil, errors.New("文件本身不是合法 JSON,先手动修好再来")
	}
	loc, err := locateTopLevel(data, "mcpServers")
	if err != nil {
		return nil, err
	}
	var servers []jsonKV
	if loc.found {
		if servers, err = orderedObject(data[loc.start:loc.end]); err != nil {
			return nil, fmt.Errorf("mcpServers 不是对象: %w", err)
		}
	}
	idx := -1
	for i, kv := range servers {
		if kv.key == srv.Name {
			idx = i
			break
		}
	}
	var oldRaw json.RawMessage
	if idx >= 0 {
		oldRaw = servers[idx].raw
	}
	newRaw, err := claudeEntry(oldRaw, srv)
	if err != nil {
		return nil, err
	}
	sp := &spliced{newBlock: renderEntry(srv.Name, newRaw, indent)}
	if idx >= 0 {
		sp.oldBlock = renderEntry(srv.Name, oldRaw, indent)
		if jsonEqual(oldRaw, newRaw) {
			sp.action = "unchanged"
			sp.out = data
			return sp, nil
		}
		sp.action = "replace"
		servers[idx].raw = newRaw
	} else {
		sp.action = "add"
		servers = append(servers, jsonKV{key: srv.Name, raw: newRaw})
	}

	value := renderObject(servers, indent, 1)
	var out []byte
	if loc.found {
		out = concat(data[:loc.start], value, data[loc.end:])
	} else {
		// 没有 mcpServers 这个键:插到开括号后面当第一个键。
		// 空对象后面不用逗号,非空的要
		ins := "\n" + indent + `"mcpServers": ` + string(value)
		if loc.empty {
			ins += "\n"
		} else {
			ins += ","
		}
		out = concat(data[:loc.afterOpen], []byte(ins), data[loc.afterOpen:])
	}
	if err := verifyClaude(out, srv.Name, newRaw); err != nil {
		return nil, err
	}
	sp.out = out
	return sp, nil
}

// topLevelLoc 顶层对象里某个键的定位结果
type topLevelLoc struct {
	found      bool
	start, end int  // 值所占的字节区间 [start, end)
	afterOpen  int  // 开括号后面那个字节,没找到键时的插入点
	empty      bool // 顶层对象是不是一个键都没有
}

// locateTopLevel 用 Decoder 的 Token 流走一遍顶层对象,记下目标键的值的字节区间。
// 只读不改,所以别的键的内容——包括大整数、转义、缩进——都不会被碰
func locateTopLevel(data []byte, key string) (topLevelLoc, error) {
	var loc topLevelLoc
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return loc, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return loc, errors.New("顶层不是 JSON 对象")
	}
	loc.afterOpen = int(dec.InputOffset())
	loc.empty = !dec.More()
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return loc, err
		}
		name, _ := k.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return loc, err
		}
		if name != key || loc.found {
			continue
		}
		// RawMessage 不含值前面的空白,而 InputOffset 停在值的末尾,倒推出起点;
		// 再对一遍字节,算错了宁可报错也不能拼出个坏文件
		end := int(dec.InputOffset())
		start := end - len(v)
		if start < 0 || !bytes.Equal(data[start:end], v) {
			return loc, errors.New("定位 mcpServers 的字节区间失败")
		}
		loc.found, loc.start, loc.end = true, start, end
	}
	return loc, nil
}

// orderedObject 把一个 JSON 对象拆成按原顺序排列的键值对。null 当空对象
func orderedObject(raw []byte) ([]jsonKV, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("不是对象")
	}
	var out []jsonKV
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := k.(string)
		if !ok {
			return nil, errors.New("键不是字符串")
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, jsonKV{key: key, raw: v})
	}
	return out, nil
}

// claudeManagedKeys 这条里由我们做主、但不属于 http 写法的键。
// 旧条目要是 stdio 写法,换成 http 时把这套去掉,不然两种传输方式混在一条里
var claudeManagedKeys = []string{"command", "args", "env", "cwd"}

// claudeEntry 生成(或在旧条目基础上更新)我们这一条,紧凑格式。
// 旧条目里我们不管的键(用户自己加的超时之类)原位保留
func claudeEntry(old json.RawMessage, srv HTTPMCPServer) (json.RawMessage, error) {
	var kvs []jsonKV
	if len(old) > 0 {
		// 原来不是对象就整个换掉
		if parsed, err := orderedObject(old); err == nil {
			kvs = parsed
		}
	}
	set := func(key string, v any) error {
		raw, err := marshalNoEscape(v)
		if err != nil {
			return err
		}
		for i := range kvs {
			if kvs[i].key == key {
				kvs[i].raw = raw
				return nil
			}
		}
		kvs = append(kvs, jsonKV{key: key, raw: raw})
		return nil
	}
	del := func(key string) {
		for i := range kvs {
			if kvs[i].key == key {
				kvs = append(kvs[:i], kvs[i+1:]...)
				return
			}
		}
	}
	if err := set("type", "http"); err != nil {
		return nil, err
	}
	if err := set("url", srv.URL); err != nil {
		return nil, err
	}
	if srv.Token != "" {
		if err := set("headers", map[string]string{"Authorization": "Bearer " + srv.Token}); err != nil {
			return nil, err
		}
	} else {
		del("headers")
	}
	for _, k := range claudeManagedKeys {
		del(k)
	}
	var b bytes.Buffer
	if err := json.Compact(&b, renderObject(kvs, "", 0)); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// renderObject 把键值对拼回一个对象。depth 是这个对象自己所在的层级:
// 它的键缩进 depth+1 层,闭括号缩进 depth 层;值里的换行按同样的规则重新缩进
func renderObject(entries []jsonKV, indent string, depth int) []byte {
	if len(entries) == 0 {
		return []byte("{}")
	}
	inner := strings.Repeat(indent, depth+1)
	outer := strings.Repeat(indent, depth)
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, kv := range entries {
		b.WriteString(inner)
		key, _ := marshalNoEscape(kv.key)
		b.Write(key)
		b.WriteString(": ")
		var vb bytes.Buffer
		if err := json.Indent(&vb, kv.raw, inner, indent); err != nil {
			vb.Reset()
			vb.Write(kv.raw)
		}
		b.Write(vb.Bytes())
		if i < len(entries)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(outer)
	b.WriteByte('}')
	return b.Bytes()
}

// renderEntry 给确认框看的那一条:`"名字": {...}`,顶格
func renderEntry(name string, raw json.RawMessage, indent string) string {
	key, _ := marshalNoEscape(name)
	var vb bytes.Buffer
	if err := json.Indent(&vb, raw, "", indent); err != nil {
		vb.Reset()
		vb.Write(raw)
	}
	return string(key) + ": " + vb.String()
}

// marshalNoEscape 不把 < > & 转成 \u00xx —— 那是给 HTML 内嵌用的,配置文件里只会碍眼
func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

func jsonEqual(a, b []byte) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// detectJSONIndent 看开括号后面第一行缩了几格,跟着文件走;看不出来就两个空格
func detectJSONIndent(data []byte) string {
	i := bytes.IndexByte(data, '{')
	if i < 0 {
		return "  "
	}
	j := bytes.IndexByte(data[i:], '\n')
	if j < 0 {
		return "  "
	}
	rest := data[i+j+1:]
	n := 0
	for n < len(rest) && (rest[n] == ' ' || rest[n] == '\t') {
		n++
	}
	if n == 0 {
		return "  "
	}
	return string(rest[:n])
}

// verifyClaude 改完再读一遍,确认拼出来的还是合法 JSON、而且我们那条在里面。
// 这是给自己上的保险:拼接算错一个字节,宁可报错也不能把坏文件写下去
func verifyClaude(out []byte, name string, want json.RawMessage) error {
	var parsed struct {
		MCP map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return fmt.Errorf("内部错误:拼出来的不是合法 JSON,没有写入: %w", err)
	}
	if got, ok := parsed.MCP[name]; !ok || !jsonEqual(got, want) {
		return errors.New("内部错误:改完的文件里没读到预期的那条,没有写入")
	}
	return nil
}

func concat(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// ---------- Codex:~/.codex/config.toml ----------

var (
	// tomlHeaderRe 表头行(已去掉首尾空白):[a.b] 或 [[a.b]],后面可以跟注释
	tomlHeaderRe = regexp.MustCompile(`^(\[\[?)\s*([^\]]*?)\s*\]\]?\s*(#.*)?$`)
	// tomlAssignRe 单行赋值(已去掉首尾空白),只认最简单的键写法;
	// 带点的键、奇怪的键都匹配不上,匹配不上就整段换,不猜
	tomlAssignRe = regexp.MustCompile(`^([A-Za-z0-9_-]+|"[^"]*"|'[^']*')\s*=\s*(.+)$`)
)

// codexManagedKeys 这段里由我们做主的键:传输方式和鉴权。
// 换掉旧段时这些丢掉(旧的可能是 stdio 写法),别的——超时之类——留着
var codexManagedKeys = map[string]bool{
	"url": true, "command": true, "args": true, "env": true, "cwd": true,
	"http_headers": true, "bearer_token_env_var": true, "env_http_headers": true,
	"http_headers_helper": true, "auth": true,
}

func spliceCodexTOML(data []byte, srv HTTPMCPServer) (*spliced, error) {
	// 原文件先得能解析。解析不了就不动:改完再坏,分不清是谁弄坏的
	var parsed map[string]any
	if len(bytes.TrimSpace(data)) > 0 {
		if _, err := toml.Decode(string(data), &parsed); err != nil {
			return nil, fmt.Errorf("文件本身解析不了,先手动修好再来: %v", err)
		}
	}
	existing := codexServer(parsed, srv.Name)

	crlf := bytes.Contains(data, []byte("\r\n"))
	eol := func(s string) string {
		if crlf {
			return s + "\r"
		}
		return s
	}
	lines := strings.Split(string(data), "\n")

	// 找我们的段:从我们的表头起,到下一张不属于我们的表头(或文件尾)止。
	// 紧跟着的子表([mcp_servers.x.env])算我们的;散落在别处的子表另算一段
	type seg struct{ s, e int }
	var segs []seg
	in := false
	for i, ln := range lines {
		m := tomlHeaderRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		ours := m[1] == "[" && keyIs(splitTOMLKey(m[2]), "mcp_servers", srv.Name)
		switch {
		case ours && !in:
			segs = append(segs, seg{s: i, e: len(lines)})
			in = true
		case !ours && in:
			segs[len(segs)-1].e = i
			in = false
		}
	}
	// 段尾的空行和注释多半是写给下一张表的,退回去留给它
	for k := range segs {
		for segs[k].e > segs[k].s+1 && blankOrComment(lines[segs[k].e-1]) {
			segs[k].e--
		}
	}

	block := codexBlock(srv)

	if len(segs) == 0 {
		if existing != nil {
			return nil, fmt.Errorf("已经用别的写法配了 %s(不是 [mcp_servers.%s] 这种表头),请手动改", srv.Name, srv.Name)
		}
		// 追加到文件尾:与上一段隔一个空行,文件以换行结尾
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > 0 {
			lines = append(lines, eol(""))
		}
		for _, b := range block {
			lines = append(lines, eol(b))
		}
		lines = append(lines, "")
		out := []byte(strings.Join(lines, "\n"))
		if err := verifyCodex(out, srv); err != nil {
			return nil, err
		}
		return &spliced{out: out, action: "add", newBlock: strings.Join(block, "\n")}, nil
	}

	var oldParts []string
	for _, sg := range segs {
		for _, ln := range lines[sg.s:sg.e] {
			oldParts = append(oldParts, strings.TrimRight(ln, "\r"))
		}
	}
	oldBlock := strings.Join(oldParts, "\n")
	if existing != nil && codexMatches(existing, srv) {
		return &spliced{out: data, action: "unchanged", oldBlock: oldBlock, newBlock: strings.Join(block, "\n")}, nil
	}

	// 第一段换成新的(不归我们管的键跟在后面留着),其余散落的子表段删掉。
	// 从后往前改,前面的下标才不会乱
	first := segs[0]
	kept := codexKeep(lines[first.s+1 : first.e])
	repl := make([]string, 0, len(block)+len(kept))
	for _, b := range block {
		repl = append(repl, eol(b))
	}
	repl = append(repl, kept...)
	for k := len(segs) - 1; k >= 0; k-- {
		sg := segs[k]
		var mid []string
		if k == 0 {
			mid = repl
		}
		next := make([]string, 0, len(lines)-(sg.e-sg.s)+len(mid))
		next = append(next, lines[:sg.s]...)
		next = append(next, mid...)
		next = append(next, lines[sg.e:]...)
		lines = next
	}
	out := []byte(strings.Join(lines, "\n"))
	if err := verifyCodex(out, srv); err != nil {
		return nil, err
	}
	preview := make([]string, 0, len(repl))
	for _, ln := range repl {
		preview = append(preview, strings.TrimRight(ln, "\r"))
	}
	return &spliced{out: out, action: "replace", oldBlock: oldBlock, newBlock: strings.Join(preview, "\n")}, nil
}

// codexKeep 旧段里值得留下的行:注释、空行、不归我们管的单行赋值。
// 碰到子表或一行放不下的值(多行数组那种)就放弃,整段换 —— 猜错一行就是坏文件
func codexKeep(body []string) []string {
	var kept []string
	for _, ln := range body {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			kept = append(kept, ln)
			continue
		}
		if tomlHeaderRe.MatchString(t) {
			return nil
		}
		m := tomlAssignRe.FindStringSubmatch(t)
		if m == nil {
			return nil
		}
		var probe map[string]any
		if _, err := toml.Decode(t, &probe); err != nil {
			return nil
		}
		if codexManagedKeys[unquoteTOMLKey(m[1])] {
			continue
		}
		kept = append(kept, ln)
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	return kept
}

// codexBlock 我们那段的每一行。鉴权走 http_headers:Codex 不接受把 token
// 直接写成 bearer_token,只认环境变量名或者静态头;一键写入不能要求用户再去设环境变量
func codexBlock(srv HTTPMCPServer) []string {
	lines := []string{
		"[mcp_servers." + srv.Name + "]",
		"url = " + tomlString(srv.URL),
	}
	if srv.Token != "" {
		lines = append(lines, "http_headers = { Authorization = "+tomlString("Bearer "+srv.Token)+" }")
	}
	return lines
}

// codexServer 解析结果里 mcp_servers.<name> 那张表;没有就 nil
func codexServer(parsed map[string]any, name string) map[string]any {
	ms, _ := parsed["mcp_servers"].(map[string]any)
	m, _ := ms[name].(map[string]any)
	return m
}

// codexMatches 已有的那张表是不是已经就是我们要写的:地址一样、鉴权一样、
// 没有别的传输 / 鉴权键在捣乱。其它键(超时之类)不管,那是用户自己加的
func codexMatches(m map[string]any, srv HTTPMCPServer) bool {
	if u, _ := m["url"].(string); u != srv.URL {
		return false
	}
	for k := range codexManagedKeys {
		if k == "url" || k == "http_headers" {
			continue
		}
		if _, has := m[k]; has {
			return false
		}
	}
	v, has := m["http_headers"]
	if srv.Token == "" {
		return !has
	}
	hdrs, _ := v.(map[string]any)
	return has && len(hdrs) == 1 && hdrs["Authorization"] == "Bearer "+srv.Token
}

func verifyCodex(out []byte, srv HTTPMCPServer) error {
	var parsed map[string]any
	if _, err := toml.Decode(string(out), &parsed); err != nil {
		return fmt.Errorf("内部错误:拼出来的不是合法 TOML,没有写入: %v", err)
	}
	m := codexServer(parsed, srv.Name)
	if m == nil || !codexMatches(m, srv) {
		return errors.New("内部错误:改完的文件里没读到预期的那段,没有写入")
	}
	return nil
}

func blankOrComment(ln string) bool {
	t := strings.TrimSpace(ln)
	return t == "" || strings.HasPrefix(t, "#")
}

// splitTOMLKey 把 a.b."c.d" 拆成段,引号里的点不算分隔
func splitTOMLKey(s string) []string {
	var (
		segs  []string
		cur   strings.Builder
		quote rune
	)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '.':
			segs = append(segs, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(segs, strings.TrimSpace(cur.String()))
}

func keyIs(segs []string, want ...string) bool {
	if len(segs) < len(want) {
		return false
	}
	for i, w := range want {
		if segs[i] != w {
			return false
		}
	}
	return true
}

func unquoteTOMLKey(k string) string {
	if len(k) >= 2 && (k[0] == '"' || k[0] == '\'') && k[len(k)-1] == k[0] {
		return k[1 : len(k)-1]
	}
	return k
}

// tomlString 基本字符串,该转义的转义
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
