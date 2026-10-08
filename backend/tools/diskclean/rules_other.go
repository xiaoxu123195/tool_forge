//go:build !windows

package diskclean

// builtinRules 其他系统上暂时没有缓存规则。界面会说明这一点,
// 大文件和重复文件两页照常能用
func builtinRules() []CacheRule { return nil }
