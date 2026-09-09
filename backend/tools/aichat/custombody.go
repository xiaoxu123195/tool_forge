package aichat

// applyCustomBody 把用户配的自定义字段并进请求体。
//
// 放在所有推断逻辑之后、序列化之前,所以它能盖掉我们自己算出来的任何东西 ——
// 这正是它存在的意义:模型能力推断、思考档位翻译、采样参数过滤都可能对某家中转猜错,
// 猜错时用户得有办法自己按对,而不是等一个新版本。
//
// 合并是浅层的:顶层键直接替换。不做深合并,因为"我要把 thinking 整个换掉"和
// "我只想改 thinking.type"看起来一模一样,猜错了比不合并更难查 ——
// 想改嵌套字段就把那一整个对象写全,行为至少是确定的。
//
// 值为 nil 表示删掉这个键。有的中转是"这个字段存在就报错",光靠覆盖救不了。
func applyCustomBody(body map[string]any, custom map[string]any) {
	if body == nil || len(custom) == 0 {
		return
	}
	for k, v := range custom {
		if k == "" {
			continue
		}
		if v == nil {
			delete(body, k)
			continue
		}
		body[k] = v
	}
}
