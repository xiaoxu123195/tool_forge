package plist

import (
	"fmt"
	"sort"
	"time"

	hplist "howett.net/plist"
)

// NSKeyedArchiver 的归档长这样:
//
//	{ "$archiver": "NSKeyedArchiver", "$version": 100000,
//	  "$top": {"root": UID(1)}, "$objects": [ "$null", {...}, {...} ] }
//
// 所有对象拍平在 $objects 里,互相之间用 UID 下标引用。
// 不拆包的话看到的就是一张平表加一堆数字,读不出任何结构。

// nsTimeEpoch NSDate 的零点是 2001-01-01 UTC,不是 Unix 的 1970
var nsTimeEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// maxUnwrapDepth 拆包递归深度上限。
// 归档里的引用可以指向任意下标,构造一条足够深的链就能把栈撑爆;
// 循环引用有 visiting 挡着,深链没有,所以这道闸得单独设
const maxUnwrapDepth = 512

// IsNSKeyedArchive 判断一个解析结果是不是 NSKeyedArchiver 归档
func IsNSKeyedArchive(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if s, ok := m["$archiver"].(string); !ok || s != "NSKeyedArchiver" {
		return false
	}
	if _, ok := m["$version"]; !ok {
		return false
	}
	if _, ok := m["$objects"].([]any); !ok {
		return false
	}
	_, ok = m["$top"].(map[string]any)
	return ok
}

// unwrapper 一次拆包的状态。
// memo 缓存已解出的对象,visiting 挡循环引用
type unwrapper struct {
	objects  []any
	memo     map[uint64]any
	visiting map[uint64]bool
	notes    []string
}

// UnwrapNSKeyedArchive 把归档拆成正常的对象树。
// 不是归档就原样返回。第二个返回值是过程中需要提醒的事。
func UnwrapNSKeyedArchive(root any) (any, []string) {
	if !IsNSKeyedArchive(root) {
		return root, nil
	}
	m := root.(map[string]any)
	u := &unwrapper{
		objects:  m["$objects"].([]any),
		memo:     map[uint64]any{},
		visiting: map[uint64]bool{},
	}
	top := m["$top"].(map[string]any)

	// $top 通常只有一个 "root" 键,那就直接把里面的东西返回去 ——
	// 多套一层 {"root": ...} 对读的人没有任何帮助
	if len(top) == 1 {
		for _, v := range top {
			return u.resolveValue(v, 0), u.notes
		}
	}
	out := map[string]any{}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = u.resolveValue(top[k], 0)
	}
	return out, u.notes
}

// resolveRef 顺着 UID 找到 $objects 里的对象并解开
func (u *unwrapper) resolveRef(uid hplist.UID, depth int) any {
	idx := uint64(uid)
	if v, ok := u.memo[idx]; ok {
		return v
	}
	if u.visiting[idx] {
		// 循环引用:归档里对象互相指是合法的(比如父子节点),
		// 展开成树时只能在第二次撞上时打个标记停住
		u.note(fmt.Sprintf("对象 #%d 存在循环引用,已在重复处截断", idx))
		return map[string]any{"__circular_uid": idx}
	}
	if idx >= uint64(len(u.objects)) {
		u.note(fmt.Sprintf("UID #%d 越出 $objects 范围(共 %d 个),归档可能已损坏", idx, len(u.objects)))
		return map[string]any{"__dangling_uid": idx}
	}
	u.visiting[idx] = true
	defer delete(u.visiting, idx)

	resolved := u.resolveValue(u.objects[idx], depth+1)
	u.memo[idx] = resolved
	return resolved
}

func (u *unwrapper) resolveValue(raw any, depth int) any {
	if depth > maxUnwrapDepth {
		u.note(fmt.Sprintf("嵌套超过 %d 层,已停止展开", maxUnwrapDepth))
		return "__max_depth__"
	}
	switch v := raw.(type) {
	case string:
		// 归档用字符串 "$null" 当空值占位
		if v == "$null" {
			return nil
		}
		return v
	case hplist.UID:
		return u.resolveRef(v, depth)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = u.resolveValue(item, depth+1)
		}
		return out
	case map[string]any:
		// 带 $class 的是一个 Objective-C 对象,按类名还原成对应的原生形状
		if cls, ok := v["$class"].(hplist.UID); ok {
			return u.unwrapClass(u.classnameOf(cls, depth), v, depth)
		}
		return u.resolveDict(v, depth)
	}
	return raw
}

func (u *unwrapper) resolveDict(v map[string]any, depth int) map[string]any {
	out := make(map[string]any, len(v))
	for k, item := range v {
		out[k] = u.resolveValue(item, depth+1)
	}
	return out
}

// classnameOf 解出 $class 指向的类名
func (u *unwrapper) classnameOf(cls hplist.UID, depth int) string {
	info, _ := u.resolveRef(cls, depth).(map[string]any)
	name, _ := info["$classname"].(string)
	return name
}

// unwrapClass 按类名把对象还原成原生形状。
//
// 覆盖的是取证里真会遇到的那些容器类。没覆盖到的类不丢弃 ——
// 原样保留字段,只把 $class 换成看得懂的 __class 类名,
// 这样即使是个陌生的自定义类,里面的数据照样读得到。
func (u *unwrapper) unwrapClass(classname string, dict map[string]any, depth int) any {
	switch classname {
	case "NSString", "NSMutableString":
		if s, ok := dict["NS.string"].(string); ok {
			return s
		}
		return u.resolveValue(dict["NS.string"], depth+1)

	case "NSNumber", "NSDecimalNumber":
		if v, ok := dict["NS.intval"]; ok {
			return u.resolveValue(v, depth+1)
		}
		if v, ok := dict["NS.dblval"]; ok {
			return u.resolveValue(v, depth+1)
		}
		return nil

	case "NSDate":
		// NS.time 是相对 2001-01-01 的秒数
		if t, ok := toFloat(dict["NS.time"]); ok {
			return nsTimeEpoch.Add(time.Duration(t * float64(time.Second)))
		}
		return u.resolveValue(dict["NS.time"], depth+1)

	case "NSData", "NSMutableData":
		return u.resolveValue(dict["NS.data"], depth+1)

	case "NSArray", "NSMutableArray", "NSSet", "NSMutableSet",
		"NSOrderedSet", "NSMutableOrderedSet":
		items, ok := dict["NS.objects"].([]any)
		if !ok {
			return []any{}
		}
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = u.resolveValue(item, depth+1)
		}
		return out

	case "NSDictionary", "NSMutableDictionary":
		keys, kok := dict["NS.keys"].([]any)
		values, vok := dict["NS.objects"].([]any)
		if !kok || !vok {
			return map[string]any{}
		}
		out := make(map[string]any, len(keys))
		for i, k := range keys {
			if i >= len(values) {
				u.note("NSDictionary 的键比值多,归档可能已损坏")
				break
			}
			out[keyString(u.resolveValue(k, depth+1))] = u.resolveValue(values[i], depth+1)
		}
		return out

	case "NSURL":
		if s, ok := dict["NS.relative"].(string); ok {
			return s
		}
		return u.resolveValue(dict["NS.relative"], depth+1)

	case "NSUUID":
		if b, ok := u.resolveValue(dict["NS.uuidbytes"], depth+1).([]byte); ok {
			return formatUUID(b)
		}
		return u.resolveValue(dict["NS.uuidbytes"], depth+1)
	}

	out := map[string]any{"__class": classname}
	for k, v := range dict {
		if k == "$class" {
			continue
		}
		out[k] = u.resolveValue(v, depth+1)
	}
	return out
}

func (u *unwrapper) note(s string) {
	// 同一句话重复几百遍没有意义,去个重
	for _, n := range u.notes {
		if n == s {
			return
		}
	}
	u.notes = append(u.notes, s)
}

// keyString 字典的键在归档里可能是任何类型,JSON 里只能是字符串
func keyString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

func formatUUID(b []byte) string {
	if len(b) != 16 {
		return fmt.Sprintf("%x", b)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
