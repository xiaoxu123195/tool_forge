// Package aistupid 封装对 aistupidlevel.info 的漂移监控接口访问。
// 接口返回当前主流 AI 模型的性能漂移检测结果（基于 CUSUM 算法）。
package aistupid

import "encoding/json"

// AxisMetric 单个评分维度（correctness/spec/codeQuality/efficiency/stability/refusal/recovery）
type AxisMetric struct {
	Value           float64 `json:"value"`
	Trend           string  `json:"trend"`           // up / down / flat
	ChangeMagnitude float64 `json:"changeMagnitude"` // 变化幅度
	Status          string  `json:"status"`          // normal / warning / degraded / critical
}

// ModelDrift 单个模型的漂移记录。
//
// 服务端实际返回的是信封形状：
//
//	{ modelId, modelName, data: { ... 真实字段 ... }, source }
//
// 自定义 UnmarshalJSON 把嵌套 data 拍平到这个结构，下游代码无需感知信封。
// 偶尔的"扁平形状"也兼容，避免以后服务端改回来时再挂。
type ModelDrift struct {
	ModelID               int        `json:"modelId"`
	ModelName             string     `json:"modelName"`
	Timestamp             string     `json:"timestamp"`
	BaselineScore         float64    `json:"baselineScore"`
	CurrentScore          float64    `json:"currentScore"`
	ConfidenceInterval    []float64  `json:"confidenceInterval"` // [lo, hi]
	Regime                string     `json:"regime"`
	Variance24h           float64    `json:"variance24h"`
	DriftStatus           string     `json:"driftStatus"` // stable / warning / degraded / critical
	PageHinkleyCUSUM      float64    `json:"pageHinkleyCUSUM"`
	LastSignificantChange string     `json:"lastSignificantChange"`
	HoursSinceChange      float64    `json:"hoursSinceChange"`
	Axes                  AxisBundle `json:"axes"`
	PrimaryIssue          string     `json:"primaryIssue"`
	Recommendation        string     `json:"recommendation"`
	Source                string     `json:"source"`
	Error                 string     `json:"error,omitempty"`
}

// UnmarshalJSON 兼容"嵌套信封"与"扁平"两种返回形状。
func (m *ModelDrift) UnmarshalJSON(b []byte) error {
	type alias ModelDrift // 避免无限递归
	var env struct {
		Data   json.RawMessage `json:"data"`
		Source string          `json:"source"`
	}
	// 先剥一层信封；解析失败直接向上抛
	if err := json.Unmarshal(b, &env); err != nil {
		return err
	}
	target := b
	if isJSONObject(env.Data) {
		target = env.Data
	}
	var a alias
	if err := json.Unmarshal(target, &a); err != nil {
		return err
	}
	*m = ModelDrift(a)
	// 信封上的 source 覆盖嵌套里的空值
	if m.Source == "" {
		m.Source = env.Source
	}
	return nil
}

// isJSONObject 判断 RawMessage 是否是一个对象字面量（忽略前导空白）
func isJSONObject(raw json.RawMessage) bool {
	for _, c := range raw {
		switch c {
		case ' ', '\n', '\r', '\t':
			continue
		}
		return c == '{'
	}
	return false
}

// AxisBundle 7 个评分维度；用具名字段方便前端类型检查
type AxisBundle struct {
	Correctness AxisMetric `json:"correctness"`
	Spec        AxisMetric `json:"spec"`
	CodeQuality AxisMetric `json:"codeQuality"`
	Efficiency  AxisMetric `json:"efficiency"`
	Stability   AxisMetric `json:"stability"`
	Refusal     AxisMetric `json:"refusal"`
	Recovery    AxisMetric `json:"recovery"`
}

// BatchMeta 整体响应的元信息
type BatchMeta struct {
	Total     int    `json:"total"`
	Cached    int    `json:"cached"`
	Computed  int    `json:"computed"`
	Errors    int    `json:"errors"`
	Partial   bool   `json:"partial"`
	Timestamp string `json:"timestamp"`
}

// BatchError 上游对个别模型的错误回报
type BatchError struct {
	ModelID   int    `json:"modelId,omitempty"`
	ModelName string `json:"modelName,omitempty"`
	Message   string `json:"message,omitempty"`
}

// DriftBatchResponse /api/drift/batch 的完整响应
type DriftBatchResponse struct {
	Success bool         `json:"success"`
	Data    []ModelDrift `json:"data"`
	Meta    BatchMeta    `json:"meta"`
	Errors  []BatchError `json:"errors,omitempty"`
	// FetchedAt 本地抓取时间（RFC3339），给前端展示"最后更新"
	FetchedAt string `json:"fetchedAt"`
}
