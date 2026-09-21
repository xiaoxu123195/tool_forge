package apitool

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// 解析 OpenAPI 3.x 和 Swagger 2.0,JSON 和 YAML 都收。
//
// 不引第三方的 OpenAPI 库:那些库为了完整支持规范会把文档严格校验一遍,
// 而现实里的文档大半有点小毛病(少一个 required、$ref 指到不存在的地方、
// 把 example 写成 examples)。严格校验的结果是整份文档直接拒绝,
// 而人只想导其中两个接口。这里反过来 —— 能认出多少认多少,认不出的逐条说明跳过了什么。

// maxRefDepth $ref 展开的层数上限。循环引用在真实文档里不罕见
const maxRefDepth = 12

var methods = []string{"get", "post", "put", "patch", "delete", "head", "options"}

// Parse 解析一份文档
func Parse(data []byte) (*ParseResult, error) {
	doc, err := decode(data)
	if err != nil {
		return nil, err
	}

	res := &ParseResult{Ops: []Op{}, Warnings: []string{}}
	if info, ok := doc["info"].(map[string]any); ok {
		res.Title = str(info["title"])
		res.Version = str(info["version"])
	}

	switch {
	case str(doc["openapi"]) != "":
		res.SpecVersion = "openapi-3"
		res.BaseURL = baseFromServers(doc)
	case str(doc["swagger"]) != "":
		res.SpecVersion = "swagger-2"
		res.BaseURL = baseFromSwagger(doc)
	default:
		return nil, fmt.Errorf("这不像 OpenAPI 或 Swagger 文档:顶层既没有 openapi 也没有 swagger 字段")
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		return nil, fmt.Errorf("文档里没有 paths,没有可导入的接口")
	}

	p := &parser{doc: doc, swagger: res.SpecVersion == "swagger-2"}
	// map 遍历无序,排一下,否则每次导入看到的顺序都不同
	for _, path := range sortedKeys(paths) {
		item, ok := paths[path].(map[string]any)
		if !ok {
			continue
		}
		// 路径级参数对该路径下所有方法生效
		shared := p.params(item["parameters"])
		for _, method := range methods {
			raw, ok := item[method].(map[string]any)
			if !ok {
				continue
			}
			op, warn := p.operation(method, path, raw, shared)
			res.Warnings = append(res.Warnings, warn...)
			if op != nil {
				res.Ops = append(res.Ops, *op)
			}
		}
	}
	if len(res.Ops) == 0 {
		return nil, fmt.Errorf("解析出 0 个接口。%s", strings.Join(res.Warnings, "；"))
	}
	res.Warnings = append(res.Warnings, p.warnings...)
	return res, nil
}

// decode JSON 和 YAML 都试。JSON 本身是 YAML 的子集,但先试 JSON 的报错更准
func decode(data []byte) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err == nil {
		return doc, nil
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("既不是合法 JSON 也不是合法 YAML: %w", err)
	}
	if doc == nil {
		return nil, fmt.Errorf("文档是空的")
	}
	// yaml.v3 在嵌套层里仍可能给出 map[any]any,统一成 map[string]any,
	// 否则后面每一处类型断言都要写两遍
	return normalize(doc).(map[string]any), nil
}

func normalize(v any) any {
	switch t := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = normalize(val)
		}
		return out
	case map[string]any:
		for k, val := range t {
			t[k] = normalize(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalize(val)
		}
		return t
	}
	return v
}

type parser struct {
	doc      map[string]any
	swagger  bool
	warnings []string
}

func (p *parser) operation(method, path string, raw map[string]any, shared []Param) (*Op, []string) {
	var warns []string
	op := &Op{
		Method:      strings.ToUpper(method),
		Path:        path,
		Summary:     str(raw["summary"]),
		Description: str(raw["description"]),
		Deprecated:  raw["deprecated"] == true,
		Params:      []Param{},
	}
	op.ID = str(raw["operationId"])
	if op.ID == "" {
		op.ID = synthID(method, path)
	}
	for _, t := range arr(raw["tags"]) {
		if s := str(t); s != "" {
			op.Tags = append(op.Tags, s)
		}
	}

	// 参数:路径级的在前,方法级的可以覆盖同名的
	seen := map[string]bool{}
	params := append(append([]Param{}, shared...), p.params(raw["parameters"])...)
	for _, prm := range params {
		key := prm.In + "\x00" + prm.Name
		if seen[key] {
			// 后面的覆盖前面的
			for i := range op.Params {
				if op.Params[i].In == prm.In && op.Params[i].Name == prm.Name {
					op.Params[i] = prm
					break
				}
			}
			continue
		}
		seen[key] = true
		op.Params = append(op.Params, prm)
	}

	// 请求体
	body, bodyRequired, w := p.body(raw)
	warns = append(warns, w...)
	if body != nil {
		if props, ok := body["properties"].(map[string]any); ok && str(body["type"]) != "" || hasProps(body) {
			required := map[string]bool{}
			for _, r := range arr(body["required"]) {
				required[str(r)] = true
			}
			props, _ = body["properties"].(map[string]any)
			for _, name := range sortedKeys(props) {
				sch, _ := props[name].(map[string]any)
				argName := name
				// body 字段和 query/path 参数重名时改名,否则两者会互相覆盖,
				// 而"参数好像没生效"是最难查的一类问题
				if seen[InQuery+"\x00"+name] || seen[InPath+"\x00"+name] || seen[InHeader+"\x00"+name] {
					argName = "body_" + name
				}
				op.Params = append(op.Params, Param{
					Name:     name,
					ArgName:  argName,
					In:       InBody,
					Required: required[name],
					Schema:   sch,
				})
			}
		} else {
			// 不是对象:整个身体当一个参数
			op.BodyRaw = true
			op.BodySchema = body
			op.Params = append(op.Params, Param{
				Name: "body", ArgName: "body", In: InBody,
				Required: bodyRequired, Schema: body,
			})
		}
	}
	return op, warns
}

func hasProps(m map[string]any) bool {
	props, ok := m["properties"].(map[string]any)
	return ok && len(props) > 0
}

// params 解析一个 parameters 数组
func (p *parser) params(v any) []Param {
	var out []Param
	for _, item := range arr(v) {
		m, ok := p.resolve(item, 0).(map[string]any)
		if !ok {
			continue
		}
		name := str(m["name"])
		in := str(m["in"])
		if name == "" || in == "" {
			continue
		}
		// Swagger 2 把请求体也塞在 parameters 里,那条单独处理
		if in == "body" || in == "formData" {
			continue
		}
		if in != InPath && in != InQuery && in != InHeader {
			// cookie 参数:HTTP 客户端这边没打算支持,跳过并说一声
			p.warnf("参数 %s 在 %s 里,暂不支持,已跳过", name, in)
			continue
		}
		var sch map[string]any
		if s, ok := p.resolve(m["schema"], 0).(map[string]any); ok {
			sch = s
		} else {
			// Swagger 2 的参数把类型直接摊在参数对象上
			sch = map[string]any{}
			for _, k := range []string{"type", "format", "enum", "items", "default", "minimum", "maximum"} {
				if val, ok := m[k]; ok {
					sch[k] = val
				}
			}
		}
		if str(m["description"]) != "" && str(sch["description"]) == "" {
			sch["description"] = m["description"]
		}
		out = append(out, Param{
			Name:     name,
			ArgName:  name,
			In:       in,
			Required: m["required"] == true || in == InPath, // 路径参数天然必填
			Schema:   p.expand(sch, 0),
		})
	}
	return out
}

// body 取请求体的 schema。返回 (schema, 是否必填, 警告)
func (p *parser) body(raw map[string]any) (map[string]any, bool, []string) {
	if p.swagger {
		for _, item := range arr(raw["parameters"]) {
			m, ok := p.resolve(item, 0).(map[string]any)
			if !ok || str(m["in"]) != "body" {
				continue
			}
			sch, _ := p.resolve(m["schema"], 0).(map[string]any)
			return p.expand(sch, 0), m["required"] == true, nil
		}
		// formData 不支持:要发 multipart,而 agent 手里多半也没有文件
		for _, item := range arr(raw["parameters"]) {
			if m, ok := item.(map[string]any); ok && str(m["in"]) == "formData" {
				return nil, false, []string{fmt.Sprintf("%s 用的是 formData,暂不支持,该接口的请求体被忽略", str(raw["operationId"]))}
			}
		}
		return nil, false, nil
	}

	rb, ok := p.resolve(raw["requestBody"], 0).(map[string]any)
	if !ok {
		return nil, false, nil
	}
	content, ok := rb["content"].(map[string]any)
	if !ok {
		return nil, false, nil
	}
	// 只要 JSON。别的类型(multipart / x-www-form-urlencoded)不支持
	for _, ct := range sortedKeys(content) {
		if !strings.Contains(ct, "json") {
			continue
		}
		mt, _ := content[ct].(map[string]any)
		sch, _ := p.resolve(mt["schema"], 0).(map[string]any)
		return p.expand(sch, 0), rb["required"] == true, nil
	}
	var types []string
	for _, ct := range sortedKeys(content) {
		types = append(types, ct)
	}
	return nil, false, []string{fmt.Sprintf("%s %s 的请求体是 %s,只支持 JSON,请求体被忽略",
		strings.ToUpper(str(raw["__method"])), str(raw["operationId"]), strings.Join(types, "/"))}
}

// resolve 跟一次 $ref。指不到的地方原样返回 —— 半份文档也比整份拒绝有用
func (p *parser) resolve(v any, depth int) any {
	m, ok := v.(map[string]any)
	if !ok || depth > maxRefDepth {
		return v
	}
	ref := str(m["$ref"])
	if ref == "" {
		return m
	}
	if !strings.HasPrefix(ref, "#/") {
		p.warnf("引用了外部文档 %s,展不开", ref)
		return map[string]any{}
	}
	cur := any(p.doc)
	for _, seg := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		node, ok := cur.(map[string]any)
		if !ok {
			p.warnf("引用 %s 指到了不存在的地方", ref)
			return map[string]any{}
		}
		cur, ok = node[seg]
		if !ok {
			p.warnf("引用 %s 指到了不存在的地方", ref)
			return map[string]any{}
		}
	}
	return p.resolve(cur, depth+1)
}

// expand 把一个 schema 里的 $ref 全部就地展开。
// 展开而不是留着引用:生成的工具要交给模型,模型不会自己去查 components
func (p *parser) expand(v any, depth int) map[string]any {
	out, _ := p.expandAny(v, depth).(map[string]any)
	return out
}

func (p *parser) expandAny(v any, depth int) any {
	if depth > maxRefDepth {
		// 循环引用:停在这里给一个宽松的占位,而不是无限展开
		return map[string]any{"type": "object", "description": "（嵌套过深，已省略）"}
	}
	switch t := v.(type) {
	case map[string]any:
		resolved, _ := p.resolve(t, 0).(map[string]any)
		if resolved == nil {
			return map[string]any{}
		}
		out := make(map[string]any, len(resolved))
		for k, val := range resolved {
			if k == "$ref" {
				continue
			}
			out[k] = p.expandAny(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = p.expandAny(val, depth+1)
		}
		return out
	}
	return v
}

func (p *parser) warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, w := range p.warnings {
		if w == msg {
			return // 同一条不重复说
		}
	}
	p.warnings = append(p.warnings, msg)
}

// InputSchema 把一个接口的参数拼成工具的入参 schema
func (o Op) InputSchema() map[string]any {
	props := map[string]any{}
	var required []string
	for _, prm := range o.Params {
		sch := prm.Schema
		if len(sch) == 0 {
			sch = map[string]any{"type": "string"}
		}
		// 把参数位置写进描述:模型看不到 in,但"这是路径的一段"会影响它怎么填
		if prm.In == InPath || prm.In == InHeader {
			sch = withNote(sch, map[ParamIn]string{
				InPath:   "（URL 路径的一部分）",
				InHeader: "（作为请求头发送）",
			}[prm.In])
		}
		props[prm.ArgName] = sch
		if prm.Required {
			required = append(required, prm.ArgName)
		}
	}
	sort.Strings(required)
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func withNote(sch map[string]any, note string) map[string]any {
	out := make(map[string]any, len(sch)+1)
	for k, v := range sch {
		out[k] = v
	}
	desc := str(out["description"])
	if desc == "" {
		out["description"] = strings.Trim(note, "（）")
	} else {
		out["description"] = desc + note
	}
	return out
}

// ---- 小工具 ----

func str(v any) string {
	s, _ := v.(string)
	return s
}

func arr(v any) []any {
	a, _ := v.([]any)
	return a
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// synthID 没有 operationId 时造一个:get_users_id
func synthID(method, path string) string {
	b := strings.Builder{}
	b.WriteString(strings.ToLower(method))
	for _, seg := range strings.Split(path, "/") {
		seg = strings.Trim(seg, "{}")
		if seg == "" {
			continue
		}
		b.WriteByte('_')
		b.WriteString(seg)
	}
	return b.String()
}

// baseFromServers OpenAPI 3 的 servers[0].url
func baseFromServers(doc map[string]any) string {
	for _, s := range arr(doc["servers"]) {
		if m, ok := s.(map[string]any); ok {
			if u := str(m["url"]); u != "" {
				return strings.TrimRight(u, "/")
			}
		}
	}
	return ""
}

// baseFromSwagger Swagger 2 是 schemes + host + basePath 三段拼起来的
func baseFromSwagger(doc map[string]any) string {
	scheme := "https"
	for _, s := range arr(doc["schemes"]) {
		if str(s) != "" {
			scheme = str(s)
			break
		}
	}
	host := str(doc["host"])
	if host == "" {
		return ""
	}
	return strings.TrimRight(scheme+"://"+host+str(doc["basePath"]), "/")
}
