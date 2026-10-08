//go:build windows

package diskclean

import (
	"path/filepath"
	"strings"
	"testing"
)

// 每条内置规则都要过守卫:新加一条规则,就算路径写错了,也不能清到受保护的地方。
// 这条测试是规则表的门槛 —— 规则的内容就是"删掉这些路径"
func TestBuiltinRulesAreSafe(t *testing.T) {
	env := fakeEnv()
	g := newGuard(windowsSpec(env))
	vars := map[string]string{
		"TEMP":         env.localAppData + `\Temp`,
		"SystemRoot":   env.systemRoot,
		"LOCALAPPDATA": env.localAppData,
		"APPDATA":      env.appData,
		"ProgramData":  env.programData,
		"USERPROFILE":  env.home,
	}
	get := func(k string) string { return vars[k] }

	rules := builtinRules()
	if len(rules) == 0 {
		t.Fatal("一条规则都没有")
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if r.ID == "" || r.Name == "" || r.Desc == "" || r.Group == "" {
			t.Errorf("%q: ID、名字、说明、分组都得有", r.ID)
		}
		if seen[r.ID] {
			t.Errorf("%q: ID 重复", r.ID)
		}
		seen[r.ID] = true
		if r.RecycleBin {
			if len(r.Paths)+len(r.Files) > 0 {
				t.Errorf("%q: 回收站规则不该再带路径", r.ID)
			}
			continue
		}
		if len(r.Paths)+len(r.Files) == 0 {
			t.Errorf("%q: 没有路径", r.ID)
		}
		check := func(raw string, contents bool) {
			if strings.Contains(raw, "..") {
				t.Errorf("%q: %s 里不能有 ..", r.ID, raw)
			}
			// 从环境变量开始,规则才不会写死某台机器的盘符和用户名
			if !strings.HasPrefix(raw, "%") {
				t.Errorf("%q: %s 要从环境变量开始", r.ID, raw)
			}
			full, ok := expandEnv(raw, get)
			if !ok || !filepath.IsAbs(full) {
				t.Errorf("%q: %s 用了取不到的变量", r.ID, raw)
				return
			}
			full = strings.ReplaceAll(full, "*", "Default")
			v := g.Check(full)
			if contents {
				v = g.CheckContents(full)
			}
			if v.Blocked {
				t.Errorf("%q: %s 过不了守卫:%s", r.ID, full, v.Reason)
			}
		}
		for _, p := range r.Paths {
			check(p, true)
		}
		for _, f := range r.Files {
			check(f, false)
		}
		for _, m := range r.Match {
			if _, err := filepath.Match(m, "x"); err != nil {
				t.Errorf("%q: 匹配模式 %q 写错了", r.ID, m)
			}
		}
	}
	// 默认勾上的只能是系统那几项:浏览器开着时多半删不动,开发缓存删了要重新下载
	for _, r := range rules {
		if r.Default && r.Group != "系统" {
			t.Errorf("%q: 只有系统那几项才默认勾上", r.ID)
		}
	}
}
