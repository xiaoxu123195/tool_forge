package devicefs

// 按文件名在设备上找东西。
//
// 光有目录树是不够用的:iOS 上有价值的文件散在几百个 UUID 命名的沙盒目录里,
// Android 上则散在几百个包名目录里 —— 靠一层层点进去找一个 .mmkv 基本没戏。
//
// 两个平台的实现都落在各自的 transport 里(见 ios.go / android.go),
// 但共同点是:递归查找交给设备上的 find 去做,不在客户端一层层问。
// 客户端递归的话每一层都是一次往返,USB 上慢到没法用。

// SearchHit 一条命中
type SearchHit struct {
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
	// ModTime Unix 秒
	ModTime int64 `json:"modTime"`
}

// SearchResult 一次查找的结果
type SearchResult struct {
	Root    string      `json:"root"`
	Pattern string      `json:"pattern"`
	Hits    []SearchHit `json:"hits"`
	// Truncated 命中太多被截断了
	Truncated bool `json:"truncated"`
	// Stderr 设备上 find 自己报的错(权限不足之类)。这些不算失败:
	// 越权的目录跳过就是了,已经找到的照样有用
	Stderr string `json:"stderr,omitempty"`
}

// searchLimit 最多回多少条
const searchLimit = 500
