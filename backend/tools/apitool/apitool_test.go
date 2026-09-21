package apitool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const spec3 = `{
  "openapi": "3.0.0",
  "info": {"title": "订单服务", "version": "1.2"},
  "servers": [{"url": "https://api.example.com/v1/"}],
  "paths": {
    "/orders/{orderId}": {
      "parameters": [
        {"name": "orderId", "in": "path", "required": true, "schema": {"type": "string"}}
      ],
      "get": {
        "operationId": "getOrder",
        "summary": "查询订单",
        "parameters": [
          {"name": "verbose", "in": "query", "schema": {"type": "boolean"}},
          {"name": "X-Trace", "in": "header", "schema": {"type": "string"}}
        ]
      },
      "patch": {
        "operationId": "updateOrder",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {"$ref": "#/components/schemas/OrderPatch"}
            }
          }
        }
      }
    },
    "/orders": {
      "post": {
        "operationId": "createOrder",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["items"],
                "properties": {
                  "items": {"type": "array", "items": {"type": "string"}},
                  "note": {"type": "string"}
                }
              }
            }
          }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "OrderPatch": {
        "type": "object",
        "required": ["status"],
        "properties": {
          "status": {"type": "string", "enum": ["paid", "shipped"]},
          "owner": {"$ref": "#/components/schemas/User"}
        }
      },
      "User": {"type": "object", "properties": {"id": {"type": "integer"}}}
    }
  }
}`

func parse(t *testing.T, s string) *ParseResult {
	t.Helper()
	res, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return res
}

func opByID(t *testing.T, res *ParseResult, id string) Op {
	t.Helper()
	for _, o := range res.Ops {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("没有解析出 %s,现有: %v", id, ids(res))
	return Op{}
}

func ids(res *ParseResult) []string {
	out := []string{}
	for _, o := range res.Ops {
		out = append(out, o.ID)
	}
	return out
}

func TestParseOpenAPI3(t *testing.T) {
	res := parse(t, spec3)
	if res.Title != "订单服务" || res.SpecVersion != "openapi-3" {
		t.Errorf("元信息不对: %+v", res)
	}
	// servers 里带结尾斜杠,拼路径时会出现 //,要去掉
	if res.BaseURL != "https://api.example.com/v1" {
		t.Errorf("baseURL = %q", res.BaseURL)
	}
	if len(res.Ops) != 3 {
		t.Fatalf("该有 3 个接口: %v", ids(res))
	}

	get := opByID(t, res, "getOrder")
	// 路径级参数要合并进每个方法
	var gotPath, gotQuery, gotHeader bool
	for _, p := range get.Params {
		switch p.In {
		case InPath:
			gotPath = p.Name == "orderId" && p.Required
		case InQuery:
			gotQuery = p.Name == "verbose"
		case InHeader:
			gotHeader = p.Name == "X-Trace"
		}
	}
	if !gotPath || !gotQuery || !gotHeader {
		t.Errorf("参数没合全: %+v", get.Params)
	}
}

// $ref 必须就地展开 —— 生成的 schema 是给模型看的,它不会自己去查 components
func TestParseExpandsRefs(t *testing.T) {
	res := parse(t, spec3)
	op := opByID(t, res, "updateOrder")
	schema := op.InputSchema()
	raw, _ := json.Marshal(schema)
	if strings.Contains(string(raw), "$ref") {
		t.Errorf("schema 里还留着 $ref: %s", raw)
	}
	props, _ := schema["properties"].(map[string]any)
	status, _ := props["status"].(map[string]any)
	if status == nil {
		t.Fatalf("body 的字段没有摊平到入参里: %s", raw)
	}
	if _, ok := status["enum"]; !ok {
		t.Errorf("enum 丢了: %v", status)
	}
	// 嵌套的 $ref 也要展开
	owner, _ := props["owner"].(map[string]any)
	if owner == nil || owner["type"] != "object" {
		t.Errorf("嵌套引用没展开: %v", owner)
	}
	// 必填 = 路径参数(天然必填)+ requestBody 里声明的那些
	req, _ := schema["required"].([]string)
	if strings.Join(req, ",") != "orderId,status" {
		t.Errorf("必填算错了: %v", req)
	}
}

// 路径参数天然必填,哪怕文档里没写 required
func TestPathParamAlwaysRequired(t *testing.T) {
	res := parse(t, `{"openapi":"3.0.0","paths":{"/a/{id}":{"get":{"operationId":"g",
	 "parameters":[{"name":"id","in":"path","schema":{"type":"string"}}]}}}}`)
	op := opByID(t, res, "g")
	if !op.Params[0].Required {
		t.Error("路径参数必须是必填的,不填就拼不出地址")
	}
}

// body 字段和 query 参数重名时要改名,否则互相覆盖
func TestBodyFieldRenamedOnCollision(t *testing.T) {
	res := parse(t, `{"openapi":"3.0.0","paths":{"/a":{"post":{"operationId":"p",
	 "parameters":[{"name":"name","in":"query","schema":{"type":"string"}}],
	 "requestBody":{"content":{"application/json":{"schema":{"type":"object",
	   "properties":{"name":{"type":"string"},"age":{"type":"integer"}}}}}}}}}}`)
	op := opByID(t, res, "p")
	var bodyArg string
	for _, p := range op.Params {
		if p.In == InBody && p.Name == "name" {
			bodyArg = p.ArgName
		}
	}
	if bodyArg != "body_name" {
		t.Errorf("重名的 body 字段该改名,得到 %q", bodyArg)
	}
	if _, ok := op.InputSchema()["properties"].(map[string]any)["age"]; !ok {
		t.Error("没重名的字段不该改名")
	}
}

// 请求体不是对象时,整个当一个参数收
func TestRawBody(t *testing.T) {
	res := parse(t, `{"openapi":"3.0.0","paths":{"/a":{"post":{"operationId":"p",
	 "requestBody":{"required":true,"content":{"application/json":{"schema":{"type":"array","items":{"type":"string"}}}}}}}}}`)
	op := opByID(t, res, "p")
	if !op.BodyRaw {
		t.Fatal("数组请求体该整个当一个参数")
	}
	props, _ := op.InputSchema()["properties"].(map[string]any)
	if _, ok := props["body"]; !ok {
		t.Errorf("该有一个 body 参数: %v", props)
	}
}

func TestParseSwagger2(t *testing.T) {
	res := parse(t, `{"swagger":"2.0","host":"api.test.cn","basePath":"/api","schemes":["https"],
	 "info":{"title":"旧服务"},
	 "paths":{"/u/{id}":{"get":{"operationId":"getU",
	   "parameters":[
	     {"name":"id","in":"path","required":true,"type":"string"},
	     {"name":"q","in":"query","type":"integer","description":"页码"}]}}}}`)
	if res.SpecVersion != "swagger-2" || res.BaseURL != "https://api.test.cn/api" {
		t.Errorf("Swagger 2 的地址拼错了: %+v", res)
	}
	op := opByID(t, res, "getU")
	props, _ := op.InputSchema()["properties"].(map[string]any)
	q, _ := props["q"].(map[string]any)
	// Swagger 2 把类型摊在参数对象上,不在 schema 里
	if q == nil || q["type"] != "integer" {
		t.Errorf("Swagger 2 的参数类型没取到: %v", q)
	}
	if q["description"] != "页码" {
		t.Errorf("描述丢了: %v", q)
	}
}

func TestParseYAML(t *testing.T) {
	res := parse(t, `openapi: 3.0.0
info:
  title: YAML 服务
paths:
  /ping:
    get:
      operationId: ping
      summary: 探活
`)
	if res.Title != "YAML 服务" || len(res.Ops) != 1 {
		t.Errorf("YAML 没解析对: %+v", res)
	}
}

// 没有 operationId 时要造一个稳定的名字
func TestSynthesizedID(t *testing.T) {
	res := parse(t, `{"openapi":"3.0.0","paths":{"/users/{id}/orders":{"get":{"summary":"x"}}}}`)
	if res.Ops[0].ID != "get_users_id_orders" {
		t.Errorf("造出来的 id = %q", res.Ops[0].ID)
	}
}

// 循环引用不能把解析器转死
func TestCyclicRefTerminates(t *testing.T) {
	done := make(chan *ParseResult, 1)
	go func() {
		res, err := Parse([]byte(`{"openapi":"3.0.0","paths":{"/a":{"post":{"operationId":"p",
		 "requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Node"}}}}}}},
		 "components":{"schemas":{"Node":{"type":"object","properties":{"child":{"$ref":"#/components/schemas/Node"}}}}}}`))
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	select {
	case res := <-done:
		if len(res.Ops) != 1 {
			t.Error("循环引用的接口该照样导出来")
		}
	case <-context.Background().Done():
	}
}

// 不认识的东西要跳过并说明,不能整份拒绝 —— 人往往只想导其中两个接口
func TestUnsupportedBitsAreSkippedNotFatal(t *testing.T) {
	res := parse(t, `{"openapi":"3.0.0","paths":{
	 "/ok":{"get":{"operationId":"ok"}},
	 "/form":{"post":{"operationId":"form","requestBody":{"content":{"multipart/form-data":{"schema":{"type":"object"}}}}}},
	 "/cookie":{"get":{"operationId":"ck","parameters":[{"name":"sid","in":"cookie","schema":{"type":"string"}}]}}}}`)
	if len(res.Ops) != 3 {
		t.Errorf("跳过的是请求体和参数,接口本身还该在: %v", ids(res))
	}
	joined := strings.Join(res.Warnings, " ")
	if !strings.Contains(joined, "multipart") || !strings.Contains(joined, "cookie") {
		t.Errorf("跳过了什么必须说清楚: %v", res.Warnings)
	}
}

func TestParseRejectsNonSpec(t *testing.T) {
	for _, bad := range []string{`{"a":1}`, `不是文档`, `{"openapi":"3.0.0"}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%q 该报错", bad)
		}
	}
}

// ---- 请求构造 ----

func handlerFor(t *testing.T, spec, opID, base string, auth Auth, secret string) *Handler {
	t.Helper()
	res := parse(t, spec)
	pack := Pack{ID: "p1", Name: "订单 svc", BaseURL: base, Auth: auth}
	return NewHandler(pack, opByID(t, res, opID), func(string) string { return secret })
}

func TestBuildBindsEveryParamPosition(t *testing.T) {
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body, _ = readAll(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	h := handlerFor(t, spec3, "getOrder", srv.URL, Auth{Kind: AuthBearer}, "tok123")
	out, err := h.Handle(context.Background(), []byte(`{"orderId":"A/B","verbose":true,"X-Trace":"t1"}`))
	if err != nil {
		t.Fatal(err)
	}
	// 比 EscapedPath 而不是 Path:后者是解码后的,看不出有没有转义。
	// 路径参数里的斜杠不转义的话会凭空多一层路径,打到别的接口上
	if got.URL.EscapedPath() != "/orders/A%2FB" {
		t.Errorf("路径参数没转义: %s", got.URL.EscapedPath())
	}
	if got.URL.Query().Get("verbose") != "true" {
		t.Errorf("query 没带上: %s", got.URL.RawQuery)
	}
	if got.Header.Get("X-Trace") != "t1" {
		t.Error("header 参数没带上")
	}
	if got.Header.Get("Authorization") != "Bearer tok123" {
		t.Errorf("认证没加上: %q", got.Header.Get("Authorization"))
	}
	if len(body) != 0 {
		t.Error("GET 不该有请求体")
	}

	var resp response
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || string(resp.Body) != `{"ok":true}` {
		t.Errorf("响应没原样带回来: %+v", resp)
	}
}

func TestBuildSendsJSONBody(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = readAll(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	h := handlerFor(t, spec3, "createOrder", srv.URL, Auth{}, "")
	if _, err := h.Handle(context.Background(), []byte(`{"items":["a","b"],"note":"急"}`)); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("请求体不是 JSON: %s", body)
	}
	if len(sent) != 2 || sent["note"] != "急" {
		t.Errorf("请求体不对: %s", body)
	}
}

// 必填没给要当场说清楚,而不是发一个注定失败的请求
func TestMissingRequiredIsRefusedBeforeSending(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()

	h := handlerFor(t, spec3, "getOrder", srv.URL, Auth{}, "")
	_, err := h.Handle(context.Background(), []byte(`{"verbose":true}`))
	if err == nil || !strings.Contains(err.Error(), "orderId") {
		t.Fatalf("该说缺了哪个参数,得到: %v", err)
	}
	if hit {
		t.Error("参数不全就不该把请求发出去")
	}
}

// 非 2xx 不算错误:4xx 的响应体里往往写着为什么不行
func TestNon2xxReturnsBodyNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error":"status 不合法"}`))
	}))
	defer srv.Close()

	h := handlerFor(t, spec3, "getOrder", srv.URL, Auth{}, "")
	out, err := h.Handle(context.Background(), []byte(`{"orderId":"1"}`))
	if err != nil {
		t.Fatalf("4xx 不该变成 Go error: %v", err)
	}
	var resp response
	_ = json.Unmarshal(out, &resp)
	if resp.Status != 422 || !strings.Contains(string(resp.Body), "不合法") {
		t.Errorf("失败的响应体要原样带回来: %+v", resp)
	}
}

func TestAuthKinds(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cases := []struct {
		auth   Auth
		secret string
		check  func(*testing.T, *http.Request)
	}{
		{Auth{Kind: AuthHeader, Name: "X-API-Key"}, "k1", func(t *testing.T, r *http.Request) {
			if r.Header.Get("X-API-Key") != "k1" {
				t.Errorf("自定义头没加: %v", r.Header)
			}
		}},
		{Auth{Kind: AuthQuery, Name: "api_key"}, "k2", func(t *testing.T, r *http.Request) {
			if r.URL.Query().Get("api_key") != "k2" {
				t.Errorf("query 认证没加: %s", r.URL.RawQuery)
			}
			// 认证塞进 query 时不能把业务参数挤掉
			if r.URL.Query().Get("verbose") != "true" {
				t.Errorf("业务参数被挤掉了: %s", r.URL.RawQuery)
			}
		}},
		{Auth{Kind: AuthBasic}, "u:p", func(t *testing.T, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok || u != "u" || p != "p" {
				t.Errorf("basic 认证不对: %q", r.Header.Get("Authorization"))
			}
		}},
		{Auth{}, "", func(t *testing.T, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Error("没配认证却加了头")
			}
		}},
	}
	for _, c := range cases {
		h := handlerFor(t, spec3, "getOrder", srv.URL, c.auth, c.secret)
		if _, err := h.Handle(context.Background(), []byte(`{"orderId":"1","verbose":true}`)); err != nil {
			t.Fatal(err)
		}
		c.check(t, got)
	}
}

// 工具名要稳定、合法、带包名前缀
func TestToolName(t *testing.T) {
	cases := map[string]string{
		"getOrder":        "api-order-svc-getorder",
		"GET /users/{id}": "api-order-svc-get-users-id",
		"中文接口":            "api-order-svc-x",
	}
	for in, want := range cases {
		if got := ToolName("Order SVC", in); got != want {
			t.Errorf("ToolName(%q) = %q, want %q", in, got, want)
		}
	}
	for _, r := range ToolName("Order SVC", "getOrder") {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			t.Errorf("工具名里有模型不认的字符: %q", r)
		}
	}
}

func readAll(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}
