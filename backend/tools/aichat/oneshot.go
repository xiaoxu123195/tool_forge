package aichat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// oneShotRequest 一次"问一句话、要一段回答"的请求。
//
// 会话流(runStream)是长跑:发事件、落盘、跑工具循环、密钥失败要换一把重来。
// 但产品里还有几个地方只需要"把 prompt 丢给模型,把回答收成一个字符串" ——
// 语言识别、会话起标题。它们没有会话、没有前端订阅、不进用量账本,
// 走 runStream 那一套只会被迫背上一堆用不上的副作用。
type oneShotRequest struct {
	Provider Provider
	ModelID  string
	Prompt   string
	// System 可选的系统提示词
	System string
	// Timeout 0 = defaultDetectTimeout
	Timeout time.Duration
	// MinimalReasoning 把思考档位压到该模型支持的最低一档。
	// 起标题这种没什么可想的活儿,让会思考的模型先想两千个 token 纯属烧钱
	MinimalReasoning bool
}

// oneShot 同步跑一次补全,返回去掉首尾空白的完整正文。
//
// 注意这里**不设** MaxTokens:对会思考的模型,一个小上限会先被思考吃光,
// 正文一个字都出不来 —— 与其给个必然踩坑的上限,不如靠 MinimalReasoning 压住开销。
func oneShot(parent context.Context, req oneShotRequest) (string, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return "", fmt.Errorf("提示词不能为空")
	}
	if req.ModelID == "" {
		return "", fmt.Errorf("未指定模型")
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultDetectTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	spec := InferModelSpec(req.Provider, req.ModelID)
	conv := Conversation{
		ID:       uuid.NewString(),
		ModelID:  req.ModelID,
		System:   req.System,
		Messages: []Message{{ID: uuid.NewString(), Role: RoleUser, Content: req.Prompt}},
	}
	if req.MinimalReasoning {
		conv.ReasoningEffort = lowestEffort(spec)
	}
	sreq := chatRequest{Provider: pickKey(req.Provider), Conv: conv, Spec: spec}

	var sb strings.Builder
	var streamErr error
	// onDone 和 onError 都会结束等待。个别协议在出错后仍会走一遍收尾逻辑,
	// 两边都直接 close(done) 就是 panic —— 用 Once 保证只关一次
	var once sync.Once
	done := make(chan struct{})
	finish := func() { once.Do(func() { close(done) }) }

	cb := streamCallbacks{
		onText:  func(d string) { sb.WriteString(d) },
		onDone:  finish,
		onError: func(err error) { streamErr = err; finish() },
	}
	go func() {
		defer finish() // 协议层若既不 done 也不 error 就返回了,别把调用方永久挂住
		switch spec.Endpoint {
		case EndpointGemini:
			streamGemini(ctx, sreq, cb)
		case EndpointAnthropic:
			streamAnthropic(ctx, sreq, cb)
		case EndpointOpenAIChat:
			streamOpenAI(ctx, sreq, false, cb)
		default:
			streamOpenAI(ctx, sreq, true, cb)
		}
	}()
	<-done
	if streamErr != nil {
		return "", streamErr
	}
	return strings.TrimSpace(sb.String()), nil
}

// lowestEffort 挑该模型支持的最省的一档思考;挑不出来返回空串(=不干预)。
// 顺序就是省钱程度:能关就关,关不掉就退而求其次。
func lowestEffort(spec ModelSpec) Effort {
	if spec.Reasoning == nil {
		return ""
	}
	for _, want := range []Effort{EffortNone, EffortMinimal, EffortLow} {
		for _, got := range spec.Reasoning.Efforts {
			if got == want {
				return want
			}
		}
	}
	return ""
}
