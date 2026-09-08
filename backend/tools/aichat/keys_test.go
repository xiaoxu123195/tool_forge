package aichat

import (
	"errors"
	"fmt"
	"testing"
)

func TestSplitKeyString(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"每行一把", "sk-a\nsk-b\nsk-c", []string{"sk-a", "sk-b", "sk-c"}},
		{"逗号分隔", "sk-a,sk-b, sk-c", []string{"sk-a", "sk-b", "sk-c"}},
		{"混合分隔与空行", "sk-a,\n\n sk-b;sk-c\t", []string{"sk-a", "sk-b", "sk-c"}},
		{"去重", "sk-a\nsk-b\nsk-a", []string{"sk-a", "sk-b"}},
		{"全空白", "  \n\t ", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitKeyString(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("切出 %v,期望 %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("第 %d 把是 %q,期望 %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// APIKeys 一旦非空就是权威值。合并语义会让"从列表里删掉一把"变成删不掉 ——
// 它会从老的 APIKey 字段里被重新捞回来。
func TestNormalizeKeysPoolIsAuthoritative(t *testing.T) {
	p := Provider{
		APIKey:  "sk-legacy",
		APIKeys: []APIKeyEntry{{ID: "k1", Key: "sk-new"}},
	}
	got := normalizeKeys(p)
	if len(got) != 1 || got[0].Key != "sk-new" {
		t.Fatalf("APIKeys 非空时不该再合并 APIKey,得到 %+v", got)
	}
}

func TestNormalizeKeysMigratesLegacyField(t *testing.T) {
	got := normalizeKeys(Provider{APIKey: "sk-a, sk-b"})
	if len(got) != 2 {
		t.Fatalf("老字段里的两把没拆开: %+v", got)
	}
	for _, e := range got {
		if e.ID == "" {
			t.Fatalf("迁移出来的条目没补 ID: %+v", e)
		}
	}
	// ID 由密钥值算出,必须稳定 —— 否则每次读盘都变,检测结果和启用状态就跟丢了
	again := normalizeKeys(Provider{APIKey: "sk-a, sk-b"})
	if again[0].ID != got[0].ID {
		t.Fatalf("同一把密钥两次算出不同 ID: %s / %s", got[0].ID, again[0].ID)
	}
}

func TestNormalizeKeysDedupesAndDropsBlank(t *testing.T) {
	got := normalizeKeys(Provider{APIKeys: []APIKeyEntry{
		{Key: "sk-a"},
		{Key: "  "},
		{Key: "sk-a"},
		{Key: " sk-b "},
	}})
	if len(got) != 2 {
		t.Fatalf("期望去重去空后剩 2 条,得到 %+v", got)
	}
	if got[1].Key != "sk-b" {
		t.Fatalf("密钥两侧空白没去掉: %q", got[1].Key)
	}
}

func TestKeyAttemptsRotatesAndCoversAll(t *testing.T) {
	p := Provider{
		ID: "prov-rotate",
		APIKeys: []APIKeyEntry{
			{ID: "k1", Key: "sk-1"},
			{ID: "k2", Key: "sk-2"},
			{ID: "k3", Key: "sk-3"},
		},
	}
	// 连续三次请求应该从三把不同的密钥开头
	firsts := map[string]bool{}
	for i := 0; i < 3; i++ {
		att := keyAttempts(p)
		if len(att) != 3 {
			t.Fatalf("备选列表应含全部 3 把,得到 %d 把", len(att))
		}
		firsts[att[0].ID] = true
		// 且每把都恰好出现一次 —— failover 不该重复试同一把
		seen := map[string]bool{}
		for _, e := range att {
			if seen[e.ID] {
				t.Fatalf("备选列表里 %s 出现了两次", e.ID)
			}
			seen[e.ID] = true
		}
	}
	if len(firsts) != 3 {
		t.Fatalf("三次请求只用到 %d 把密钥打头,轮转没生效", len(firsts))
	}
}

func TestKeyAttemptsSkipsDisabled(t *testing.T) {
	p := Provider{
		ID: "prov-disabled",
		APIKeys: []APIKeyEntry{
			{ID: "k1", Key: "sk-1", Disabled: true},
			{ID: "k2", Key: "sk-2"},
		},
	}
	for i := 0; i < 4; i++ {
		att := keyAttempts(p)
		if len(att) != 1 || att[0].ID != "k2" {
			t.Fatalf("停用的密钥被选中了: %+v", att)
		}
	}
}

// 池子空时不能返回空切片 —— 调用方会直接取 [0]。
// 让它照常走"密钥为空"的报错路径,而不是在这里 panic。
func TestKeyAttemptsEmptyPool(t *testing.T) {
	att := keyAttempts(Provider{ID: "prov-empty"})
	if len(att) != 1 || att[0].Key != "" {
		t.Fatalf("空池应返回一个空密钥占位,得到 %+v", att)
	}
}

func TestIsKeyLevelError(t *testing.T) {
	retry := []error{
		fmt.Errorf("HTTP 401: Incorrect API key provided"),
		fmt.Errorf("HTTP 429: Rate limit reached"),
		fmt.Errorf("HTTP 403: permission denied"),
		errors.New("You exceeded your current quota"),
		errors.New("账户余额不足"),
	}
	for _, err := range retry {
		if !isKeyLevelError(err) {
			t.Fatalf("应判为可换密钥重试: %v", err)
		}
	}
	// 换把密钥也解决不了的,重试只是白白多打几次请求
	keep := []error{
		nil,
		fmt.Errorf("HTTP 404: model not found"),
		fmt.Errorf("HTTP 400: unsupported parameter: temperature"),
		errors.New("读取流失败: unexpected EOF"),
	}
	for _, err := range keep {
		if isKeyLevelError(err) {
			t.Fatalf("不该为它换密钥重试: %v", err)
		}
	}
}
