package diskclean

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCustomRules(t *testing.T) {
	s := testService()
	s.customPath = filepath.Join(t.TempDir(), "rules.json")
	cache := filepath.Join(t.TempDir(), "emulator", "cache")
	old := writeN(t, filepath.Join(cache, "old.bin"), 100)
	age(t, old, 72*time.Hour)
	fresh := writeN(t, filepath.Join(cache, "new.bin"), 100)

	r, err := s.SaveCustomRule(CustomRule{Dir: cache, MinAgeDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.Name != "cache" {
		t.Fatalf("没起名字或者没给 ID:%+v", r)
	}
	if _, err := s.SaveCustomRule(CustomRule{Dir: cache}); err == nil {
		t.Fatal("同一个目录加两次应该拒绝")
	}
	// 只校验、不删,守卫万一失效也不会真去动家目录
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := s.SaveCustomRule(CustomRule{Dir: home}); err == nil {
			t.Fatal("个人目录不能当缓存加进来")
		}
	}
	if _, err := s.SaveCustomRule(CustomRule{Dir: "relative"}); err == nil {
		t.Fatal("相对路径应该拒绝")
	}

	scan, _ := s.ScanCache("")
	var item *CacheItem
	for i := range scan.Items {
		if scan.Items[i].ID == customPrefix+r.ID {
			item = &scan.Items[i]
		}
	}
	if item == nil || !item.Custom || item.Group != "自定义" || item.Files != 1 {
		t.Fatalf("自定义规则在缓存列表里不对(只该算一天以前的那一个):%+v", item)
	}

	res, _ := s.CleanCache("", []string{customPrefix + r.ID})
	if res.Deleted != 1 || exists(old) || !exists(fresh) {
		t.Fatalf("应该只删掉旧的那个:%+v", res)
	}

	if err := s.DeleteCustomRule(r.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.CustomRules()) != 0 {
		t.Fatal("删掉的规则还在")
	}
}

// 存下来的规则文件被改成了个人目录:清的时候照样要拦。
// 这里只调 resolve(只展开、不删)
func TestCustomRuleFileTamperedIsStillGuarded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	s := testService()
	s.customPath = filepath.Join(t.TempDir(), "rules.json")
	data := `[{"id":"x","name":"坏规则","dir":` + strings.ReplaceAll(`"`+home+`"`, `\`, `\\`) + `,"minAgeDays":0}]`
	if err := os.WriteFile(s.customPath, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.allRules() {
		if r.ID == customPrefix+"x" {
			if got := s.resolve(r); len(got.dirs) != 0 {
				t.Fatalf("被改坏的规则展开出了可清的目录:%v", got.dirs)
			}
			return
		}
	}
	t.Fatal("规则文件里的规则没读出来")
}
