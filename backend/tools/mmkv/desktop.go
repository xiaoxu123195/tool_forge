package mmkv

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// 桌面工具页的入口。
//
// 和 MCP 那头走的是同一个 Parse / Decrypt —— 这正是把解析从前端搬过来的意义:
// 之前浏览器里一份 TypeScript、Go 里一份,同一个文件两边解出不一样的结果时
// 谁也不知道该信哪个。现在只有一份。
//
// 和 handler.go 的区别只在"给谁看":
//   - 走路径而不是把文件内容塞过桥。50MB 的 MMKV 转成 base64 是 66MB 的字符串,
//     没必要为了让 Go 读个文件先在 JS 里把它读一遍
//   - 不做 maxEntries 截断:人是要在表格里翻的,少给一条都是漏
//   - 十六进制预览给得比 MCP 长(见 decode.go)

// FileResult 桌面页需要的全部信息
type FileResult struct {
	// Name 文件名,给标题栏用
	Name string `json:"name"`
	// Size 文件字节数
	Size int64 `json:"size"`
	// Encrypted 这次是不是走了解密
	Encrypted bool `json:"encrypted"`

	DBSize       int `json:"dbSize"`
	Consumed     int `json:"consumed"`
	RemovedCount int `json:"removedCount"`

	Entries []Entry `json:"entries"`
}

// maxDesktopFileSize 桌面页打开的文件上限。
// 和前端原来的 50MB 一致 —— 再大的话表格本身就渲染不动了
const maxDesktopFileSize = 50 << 20

// ParseFile 读一个 MMKV 文件并解析。
// crcPath 和 keyHex 都非空时先解密;都为空就当明文读。
func ParseFile(path, crcPath, keyHex string) (*FileResult, error) {
	if path == "" {
		return nil, errors.New("没有选择文件")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s 是个目录", path)
	}
	if st.Size() > maxDesktopFileSize {
		return nil, fmt.Errorf("文件 %.2f MB,超过 %d MB 的上限",
			float64(st.Size())/(1<<20), maxDesktopFileSize>>20)
	}
	data, err := decryptIfNeeded(path, crcPath, keyHex)
	if err != nil {
		return nil, err
	}
	encrypted := keyHex != ""

	res, err := ParseWithHexLimit(data, desktopHexLimit)
	if err != nil {
		return nil, err
	}
	// 一个键都没解出来时,得说清楚是哪种"没有" —— 这两种情况的下一步完全不同。
	//
	// 拿真机上的 MMKV 试过一轮才发现:读不出键最常见的原因根本不是加密,
	// 而是文件整个是 0。MMKV 建存储时就把 mmap 的页预分配好了,
	// 应用没往里写过东西的话文件就是一整片零 —— 这种情况让人去找 AES key
	// 是把人往沟里带。
	if len(res.Entries) == 0 && res.RemovedCount == 0 {
		switch {
		case encrypted:
			return nil, errors.New("解密后仍然一个键都没读到,多半是 AES key 不对")
		case isBlank(data):
			return nil, errors.New("这个 MMKV 是空的 —— 文件整个是 0," +
				"说明应用建了这个存储但一次都没写过。不是加密,也不是解析失败")
		default:
			return nil, errors.New("没有解析出任何 key,但文件里是有内容的。" +
				"多半是 AES 加密的,用「加密打开」提供 .crc 和 AES key 再试")
		}
	}

	return &FileResult{
		Name:         filepath.Base(path),
		Size:         st.Size(),
		Encrypted:    encrypted,
		DBSize:       res.DBSize,
		Consumed:     res.Consumed,
		RemovedCount: res.RemovedCount,
		Entries:      res.Entries,
	}, nil
}

// isBlank 文件开头是不是一整片 0。
//
// 只看开头 64 字节:MMKV 的头(4 字节 dbSize + 一个 varint)在最前面,
// 它俩都是 0 就说明这个存储从来没被写过。整个文件扫一遍没必要 ——
// 预分配的页可能有几十 KB
func isBlank(data []byte) bool {
	n := min(len(data), 64)
	if n == 0 {
		return true
	}
	for _, b := range data[:n] {
		if b != 0 {
			return false
		}
	}
	return true
}

// readMMKV 读文件、按需解密,得到可以交给 scanEntries 的明文
func readMMKV(path, crcPath, keyHex string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("没有选择文件")
	}
	return decryptIfNeeded(path, crcPath, keyHex)
}

// decryptIfNeeded 读文件;给了 key + crc 就先解密。
//
// 和 handler.go 同一条规矩:key 和 crc 必须一起给。只给一个的后果是
// "解析得通但内容全是乱码",比直接报错难查得多
func decryptIfNeeded(path, crcPath, keyHex string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if keyHex == "" && crcPath == "" {
		return data, nil
	}
	if keyHex == "" || crcPath == "" {
		return nil, errors.New("解密需要同时给 AES key 和 .crc 文件 ——" +
			"IV 存在 .crc 里,缺一个都解不开")
	}
	crc, err := os.ReadFile(crcPath)
	if err != nil {
		return nil, err
	}
	return Decrypt(data, crc, keyHex)
}

// ValueHex 取某一个值的完整十六进制。
//
// 为什么要单独一条路:表格里带的那份是截断过的(desktopHexLimit)。
// 一个文件几千个值,每个都带完整十六进制的话,传过桥的数据能比文件本身还大。
// 所以平时给摘要,真要看完整字节时再回来读一次 —— 这是个明确的用户动作,
// 多花一次读文件的时间是划算的。
func ValueHex(path, crcPath, keyHex, key string, index int) (string, error) {
	data, err := readMMKV(path, crcPath, keyHex)
	if err != nil {
		return "", err
	}
	sc, err := scanEntries(data)
	if err != nil {
		return "", err
	}
	vals, ok := sc.byKey[key]
	if !ok {
		return "", fmt.Errorf("文件里没有键 %q", key)
	}
	if index < 0 || index >= len(vals) {
		return "", fmt.Errorf("键 %q 只有 %d 个历史值,取不到第 %d 个", key, len(vals), index)
	}
	return hex.EncodeToString(vals[index]), nil
}
