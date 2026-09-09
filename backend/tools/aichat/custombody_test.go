package aichat

import (
	"net/http"
	"reflect"
	"testing"
)

func TestApplyCustomBody(t *testing.T) {
	t.Run("覆盖我们算出来的字段", func(t *testing.T) {
		body := map[string]any{"model": "x", "stream": true, "temperature": 0.7}
		applyCustomBody(body, map[string]any{"temperature": 0.1, "top_k": 40})
		want := map[string]any{"model": "x", "stream": true, "temperature": 0.1, "top_k": 40}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("得到 %v, 期望 %v", body, want)
		}
	})

	t.Run("nil 值表示删掉这个键", func(t *testing.T) {
		// 有的中转是"这个字段存在就报错",光靠覆盖救不了
		body := map[string]any{"model": "x", "reasoning": map[string]any{"effort": "high"}}
		applyCustomBody(body, map[string]any{"reasoning": nil})
		if _, ok := body["reasoning"]; ok {
			t.Fatalf("reasoning 应该被删掉了: %v", body)
		}
	})

	t.Run("空配置不动请求体", func(t *testing.T) {
		body := map[string]any{"model": "x"}
		applyCustomBody(body, nil)
		applyCustomBody(body, map[string]any{})
		if len(body) != 1 || body["model"] != "x" {
			t.Fatalf("不该被动过: %v", body)
		}
	})

	t.Run("空键跳过", func(t *testing.T) {
		body := map[string]any{"model": "x"}
		applyCustomBody(body, map[string]any{"": 1})
		if len(body) != 1 {
			t.Fatalf("空键不该写进去: %v", body)
		}
	})

	t.Run("body 为 nil 不 panic", func(t *testing.T) {
		applyCustomBody(nil, map[string]any{"a": 1})
	})
}

// 这个面板就是拿来截图发给别人问的,漏一处就是把密钥发出去了
func TestTraceRedaction(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-proj-abcdefghijklmnop")
	h.Set("X-Api-Key", "sk-ant-0123456789")
	h.Set("Content-Type", "application/json")

	got := redactHeaders(h)
	joined := ""
	for _, l := range got {
		joined += l + "\n"
	}
	for _, leaked := range []string{"abcdefghijklmnop", "0123456789", "sk-proj-abcdefghijklmnop"} {
		if contains(joined, leaked) {
			t.Fatalf("密钥泄漏了: %q\n%s", leaked, joined)
		}
	}
	// 前 4 位要留着:多把密钥轮换时"失败的是哪一把"光看星号答不了
	if !contains(joined, "Bearer sk-p****") {
		t.Fatalf("掩码格式不对(应保留前 4 位):\n%s", joined)
	}
	if !contains(joined, "application/json") {
		t.Fatalf("非敏感头不该被动:\n%s", joined)
	}
}

func TestRedactURL(t *testing.T) {
	// Gemini 把密钥放在 query 上,只脱请求头等于没脱
	got := redactURL("https://generativelanguage.googleapis.com/v1beta/models/x:streamGenerateContent?alt=sse&key=AIzaSyABCDEFG")
	if contains(got, "AIzaSyABCDEFG") {
		t.Fatalf("URL 里的密钥没脱: %s", got)
	}
	if !contains(got, "AIza****") {
		t.Fatalf("掩码不对: %s", got)
	}
	if !contains(got, "alt=sse") {
		t.Fatalf("其余参数不该被动: %s", got)
	}
	// 没有密钥的 URL 原样返回,不该被 url.Parse 重排成另一个样子
	plain := "https://api.openai.com/v1/responses"
	if redactURL(plain) != plain {
		t.Fatalf("不含密钥的 URL 被改了: %s", redactURL(plain))
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
