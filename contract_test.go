package main

import (
	"reflect"
	"strings"
	"testing"
)

// Wails 的 BoundMethod.Call 对返回值的处理是硬编码的:
//
//	1 个返回值:是 error 就转成 reject,否则原样给 JS
//	2 个返回值:第一个给 JS;第二个只有在它是 error 时才转成 reject,
//	           否则 **静默丢弃** —— (T, string) 里的错误字符串永远到不了前端
//	3 个及以上:直接不处理
//
// 这个坑在本项目里连续造成过三次事故:密钥列表白屏、新建供应商不跳转、
// 二十几个"保存失败"弹窗从来弹不出来。人记不住,所以用测试挡住:
// 任何人再写一个 (T, string) 的 RPC,这里当场红。
func TestAppMethodsReturnRealErrors(t *testing.T) {
	errType := reflect.TypeOf((*error)(nil)).Elem()
	typ := reflect.TypeOf(&App{})

	var bad []string
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		mt := m.Type
		switch mt.NumOut() {
		case 0:
			// 没有返回值,无所谓
		case 1:
			// 单返回值 Wails 能正确传递:error 转 reject,其余原样给 JS。
			// 返回 string 当错误用(空串=成功)虽然老派,但语义完整,不拦。
		case 2:
			if !mt.Out(1).Implements(errType) {
				bad = append(bad, m.Name+" 的第二个返回值是 "+mt.Out(1).String()+
					",不是 error —— 它会在 Wails 边界被静默丢弃")
			}
		default:
			bad = append(bad, m.Name+" 有 "+itoa(mt.NumOut())+
				" 个返回值 —— Wails 只处理 1-2 个,多出来的全部丢失")
		}
	}
	if len(bad) > 0 {
		t.Fatalf("发现 %d 个会丢返回值的 RPC 签名:\n  %s\n\n"+
			"修法:第二个返回值改成 error(出错前端收到 reject),"+
			"或者合并成单返回值。", len(bad), strings.Join(bad, "\n  "))
	}
}

func itoa(n int) string {
	return string(rune('0' + n))
}
