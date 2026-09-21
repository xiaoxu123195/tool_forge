// Package apitool 把 OpenAPI 文档里的接口包装成 AI 可调用的工具。
//
// 做这件事的理由:一个内部服务已经有 OpenAPI 文档了,想让 agent 能调它,
// 现在的办法是另写一个 MCP server 把每个接口再包一遍。文档里明明什么都有 ——
// 路径、方法、参数、类型、说明 —— 重新写一遍纯属抄写。
//
// 生成出来的工具挂在本地 API server 上,所以它同时出现在两个地方:
// HTTP 接口(给脚本用)和 MCP 端点(给 Claude Code / Codex 用)。
// 和内置工具一样要在「本地 API」页里逐个勾选才会对外暴露 —— 导入不等于开放。
//
// 持久化: ~/.toolforge/api-tools.json;认证凭据不在里面,走系统凭据库
package apitool

// ParamIn 参数放在请求的哪个位置
type ParamIn = string

const (
	InPath   ParamIn = "path"
	InQuery  ParamIn = "query"
	InHeader ParamIn = "header"
	InBody   ParamIn = "body"
)

// Param 一个参数:它在接口里叫什么、放在哪、长什么样
type Param struct {
	// Name 接口侧的原名(URL 模板里的占位符名 / query 键 / header 名 / body 字段名)
	Name string `json:"name"`
	// ArgName 工具入参里的名字。多数时候和 Name 一样;
	// body 字段和 query 参数重名时,body 那个会被改名,否则会互相覆盖
	ArgName  string         `json:"argName"`
	In       ParamIn        `json:"in"`
	Required bool           `json:"required"`
	Schema   map[string]any `json:"schema,omitempty"`
}

// Op 一个接口,以及把它变成工具所需的一切
type Op struct {
	// ID 在一个包内唯一,来自 operationId;没有就用 方法+路径 造一个
	ID     string `json:"id"`
	Method string `json:"method"`
	// Path URL 模板,形如 /users/{id}
	Path        string   `json:"path"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Params      []Param  `json:"params"`
	// BodyRaw 请求体不是对象(数组、字符串之类),整个当一个名为 body 的参数收
	BodyRaw bool `json:"bodyRaw,omitempty"`
	// BodySchema BodyRaw 时的 schema
	BodySchema map[string]any `json:"bodySchema,omitempty"`
	// Deprecated 文档里标了废弃。照样能导入,但要让人看见
	Deprecated bool `json:"deprecated,omitempty"`
}

// AuthKind 认证方式
type AuthKind = string

const (
	AuthNone AuthKind = ""
	// AuthBearer Authorization: Bearer <token>
	AuthBearer AuthKind = "bearer"
	// AuthHeader 自定义头,如 X-API-Key: <token>
	AuthHeader AuthKind = "header"
	// AuthQuery 拼在 query 上,如 ?api_key=<token>
	AuthQuery AuthKind = "query"
	// AuthBasic Authorization: Basic base64(user:pass),凭据里存 "user:pass"
	AuthBasic AuthKind = "basic"
)

// Auth 认证配置。真正的密钥不在这儿 —— 它在系统凭据库里,这里只记怎么用
type Auth struct {
	Kind AuthKind `json:"kind"`
	// Name AuthHeader 时是头名,AuthQuery 时是参数名
	Name string `json:"name,omitempty"`
	// HasSecret 凭据库里有没有存过。界面据此显示"已配置"而不是把密钥读出来
	HasSecret bool `json:"hasSecret"`
}

// Pack 一次导入的结果:一份文档里选中的那几个接口
type Pack struct {
	ID string `json:"id"`
	// Name 显示名,同时作为工具名前缀。限定 [a-z0-9-]
	Name string `json:"name"`
	// BaseURL 请求发到哪儿。文档里的 servers 只是默认值,内网服务经常要改
	BaseURL string `json:"baseUrl"`
	// Source 这份文档是从哪儿来的(文件路径或 URL),只为让人记得
	Source string `json:"source,omitempty"`
	// SpecTitle 文档自报的标题
	SpecTitle string `json:"specTitle,omitempty"`
	Auth      Auth   `json:"auth"`
	// Headers 每次请求都带上的固定头(非密钥的那些,如 Accept-Language)
	Headers map[string]string `json:"headers,omitempty"`
	// TimeoutSec 单次请求超时,0 用默认
	TimeoutSec int   `json:"timeoutSec,omitempty"`
	Ops        []Op  `json:"ops"`
	CreatedAt  int64 `json:"createdAt"`
	UpdatedAt  int64 `json:"updatedAt"`
}

// ParseResult 解析一份文档的结果,给导入界面挑选用
type ParseResult struct {
	Title   string `json:"title"`
	Version string `json:"version"`
	// BaseURL 从 servers / host 推出来的默认地址
	BaseURL string `json:"baseUrl"`
	// SpecVersion "openapi-3" 或 "swagger-2"
	SpecVersion string `json:"specVersion"`
	Ops         []Op   `json:"ops"`
	// Warnings 解析时跳过了什么。跳过的接口在界面上是看不见的,必须说
	Warnings []string `json:"warnings"`
}

// keyringKey 一个包的凭据在系统凭据库里的键
func keyringKey(packID string) string { return "apitool:" + packID }
