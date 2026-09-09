package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Go 的 nil 切片过 JSON 是 null,不是 []。前端拿到 null 再去读 .length / .map
// 就是一次整页白屏 —— 而且只在"恰好一条都没有"那条分支上发生,平时测不出来。
// 跨会话搜索就这么崩过一次:一条只命中标题的会话,hits 是 nil。
//
// 这个测试把所有返回切片的 RPC 都实际调一遍,断言拿到的不是 nil,
// 并且递归检查返回值里的嵌套切片字段。
// 跟 contract_test.go 是一类东西:那个拦"错误被静默吞掉",这个拦"整页白屏"。

// unsafeToCall 不能在测试里自动调的方法,以及为什么。
//
// 名单要写理由:以后有人想往里加,得先说清楚这个方法为什么不能被调,
// 而不是"调了会失败所以先屏蔽掉" —— 那正是漏洞溜进来的方式。
var unsafeToCall = map[string]string{
	"PickHashFiles":                "会弹原生文件选择框,测试会卡住",
	"RefreshOutlookTokens":         "会真的发网络请求刷新 token",
	"PreviewOutlookExport":         "会扫全部邮件,慢且有副作用",
	"SaveAIProviderKeys":           "写操作,会改用户的密钥配置",
	"CheckAIProviderKeys":          "会真的拿密钥去打供应商接口",
	"ListOutlookActiveRefreshJobs": "依赖后台任务状态,单独跑没有意义",
}

func TestSliceReturningRPCsNeverNil(t *testing.T) {
	// 这些方法多数会懒加载配置、甚至补写默认值。指到临时目录去,
	// 别在跑测试时动用户真实的 ~/.toolforge
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	app := NewApp()
	// NewApp 会开着一些东西不放(llm-proxy 的 SQLite 日志库就是一个)。
	// Windows 上文件被占着就删不掉,t.TempDir 的清理会失败、整条用例跟着红。
	// Cleanup 是后进先出的:这行注册在 TempDir 之后,所以先于它执行
	t.Cleanup(func() { app.shutdown(context.Background()) })

	v := reflect.ValueOf(app)
	typ := v.Type()

	var checked, skipped int
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		mt := m.Type
		if mt.NumOut() == 0 || mt.Out(0).Kind() != reflect.Slice {
			continue
		}
		if why, bad := unsafeToCall[m.Name]; bad {
			t.Logf("跳过 %s(%s)", m.Name, why)
			skipped++
			continue
		}
		// 只调参数全是字符串的:传空串对"按 ID 查"的方法就是"查不到",
		// 正是最容易返回 nil 的那条分支。别的参数类型不好凭空造,遇到再说
		args := make([]reflect.Value, 0, mt.NumIn()-1)
		ok := true
		for a := 1; a < mt.NumIn(); a++ {
			if mt.In(a).Kind() != reflect.String {
				ok = false
				break
			}
			args = append(args, reflect.ValueOf(""))
		}
		if !ok {
			t.Logf("跳过 %s(参数类型暂不支持自动构造)", m.Name)
			skipped++
			continue
		}

		out := v.Method(i).Call(args)
		checked++
		if out[0].IsNil() {
			t.Errorf("%s 返回了 nil 切片 —— 前端会拿到 null,再 .length 就是整页白屏。"+
				"在 app.go 里改成返回空切片", m.Name)
		}
		for _, bad := range findNilRequiredSlices(out[0], 0) {
			t.Errorf("%s: %s", m.Name, bad)
		}
	}
	t.Logf("检查了 %d 个返回切片的 RPC,跳过 %d 个", checked, skipped)
	if checked == 0 {
		t.Fatal("一个都没检查到 —— 反射扫描多半坏了")
	}
}

// findNilRequiredSlices 递归找出"没有 omitempty 却是 nil"的切片字段。
//
// 这条判据是精确的,不是拍脑袋:
//
//	有 omitempty:nil 切片序列化时整个键都不出现 → 前端拿到 undefined,
//	             TS 那边也写成 `x?: T[]`,`?.` / `?? []` 天然覆盖到。
//	没 omitempty:nil 切片序列化成 `"x": null` → 而 TS 类型是 `x: T[]`
//	             (必填数组)。类型说一定是数组,到手却是 null,
//	             `x.length` 当场炸。
//
// 跨会话搜索崩的正是后者:SearchResult.Hits 的 tag 是 `json:"hits"`,没有 omitempty。
//
// 返回问题清单而不是直接 t.Errorf,是为了这个函数本身可测 —— 一个从没见过它
// 报错的检查器,和没有这个检查器没区别。
func findNilRequiredSlices(v reflect.Value, depth int) []string {
	if depth > 4 { // 结构不会嵌那么深,这里纯粹防自引用类型转不出来
		return nil
	}
	var out []string
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if !v.IsNil() {
			out = append(out, findNilRequiredSlices(v.Elem(), depth+1)...)
		}
	case reflect.Slice:
		// 抽查前 20 个。同一个类型的元素形状一样,遍历几千条只是更慢
		for i := 0; i < v.Len() && i < 20; i++ {
			out = append(out, findNilRequiredSlices(v.Index(i), depth+1)...)
		}
	case reflect.Struct:
		typ := v.Type()
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			if f.Type.Kind() == reflect.Slice &&
				!strings.Contains(tag, ",omitempty") &&
				v.Field(i).IsNil() {
				out = append(out, fmt.Sprintf(
					"%s.%s 是 nil,而 json tag %q 没有 omitempty —— 会序列化成 null,"+
						"前端却按必填数组用。要么保证它非 nil,要么给 tag 加 omitempty",
					typ.Name(), f.Name, tag))
			}
			out = append(out, findNilRequiredSlices(v.Field(i), depth+1)...)
		}
	}
	return out
}

// 检查器自己也得被检查一遍:它从没报过错的话,上面那条用例通过说明不了什么
func TestFindNilRequiredSlicesDetects(t *testing.T) {
	type inner struct {
		Required []string `json:"required"`           // nil → 会被逮住
		Optional []string `json:"optional,omitempty"` // nil → 放过
		Filled   []string `json:"filled"`             // 非 nil → 放过
		Ignored  []string `json:"-"`                  // 压根不进 JSON
		hidden   []string // 未导出
	}
	type outer struct {
		Items []inner `json:"items"`
	}

	got := findNilRequiredSlices(reflect.ValueOf(outer{Items: []inner{{
		Filled: []string{},
	}}}), 0)
	if len(got) != 1 {
		t.Fatalf("应该只逮住 Required 一个,得到 %d 条:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[0], "Required") {
		t.Errorf("逮错了字段: %s", got[0])
	}

	// 全都填好时不该有任何抱怨
	clean := findNilRequiredSlices(reflect.ValueOf(outer{Items: []inner{{
		Required: []string{}, Filled: []string{},
	}}}), 0)
	if len(clean) != 0 {
		t.Errorf("干净的结构不该报错:\n%s", strings.Join(clean, "\n"))
	}

	// outer.Items 自己是 nil 也要被逮住(顶层就漏了的情况)
	if got := findNilRequiredSlices(reflect.ValueOf(outer{}), 0); len(got) != 1 {
		t.Errorf("顶层 nil 切片也该被逮住,得到 %d 条", len(got))
	}
}

// 上面那条用例跑在空目录上,大多数 RPC 返回空列表 —— 嵌套检查根本没东西可看。
// 这条把出过事的那个场景原样摆出来:一条**标题**能匹配、正文一处都匹配不上的会话。
// 当时 SearchResult.Hits 就是在这条路上留成 nil 的。
func TestSearchResultHitsNeverNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".toolforge", "ai-chat", "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 标题里有"逆子",正文里没有 —— 命中标题、hits 为空,正是那条分支
	conv := `{"id":"c1","title":"逆子AI 的会话","messages":[
		{"id":"m1","role":"user","content":"完全无关的内容","createdAt":1}
	],"createdAt":1,"updatedAt":2}`
	if err := os.WriteFile(filepath.Join(dir, "c1.json"), []byte(conv), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })

	got, err := app.SearchAIConversations("逆子")
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("应该命中一条会话,得到 %d 条", len(got))
	}
	if !got[0].TitleMatch {
		t.Error("命中的是标题,titleMatch 应该为真")
	}
	for _, bad := range findNilRequiredSlices(reflect.ValueOf(got), 0) {
		t.Errorf("SearchAIConversations: %s", bad)
	}
}
