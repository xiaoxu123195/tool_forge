package devicefs

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 按文件名在设备上找东西。
//
// 光有目录树是不够用的:iOS 上有价值的文件散在几百个 UUID 命名的沙盒目录里,
// 靠一层层点进去找一个 .mmkv 基本没戏。
//
// 这条走 SSH exec 而不是 SFTP —— SFTP 协议里没有"递归查找"这回事,
// 自己在客户端递归的话每一层都是一次往返,USB 上慢到没法用。
// 设备上一条 find 就完事了。

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
	// Stderr find 自己报的错(权限不足之类)。这些不算失败:
	// 越权的目录跳过就是了,已经找到的照样有用
	Stderr string `json:"stderr,omitempty"`
}

const (
	// searchTimeout 全盘 find 在 USB 上是分钟级的,给足但不能无限等
	searchTimeout = 90 * time.Second
	// searchLimit 最多回多少条
	searchLimit = 500
)

// Search 在设备上按文件名查找(不区分大小写,支持 * 通配)
func (m *Manager) Search(sessionID, root, pattern string, limit int) (*SearchResult, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errNeedPattern
	}
	root = cleanRemote(root)
	if root == "" {
		root = s.StartPath
	}
	if limit <= 0 || limit > searchLimit {
		limit = searchLimit
	}
	// 人敲的是"想找什么",不是 shell 通配符。两头自动补 * ——
	// 不补的话搜 "mmkv" 一条都出不来,而那正是最常见的用法
	if !strings.ContainsAny(pattern, "*?") {
		pattern = "*" + pattern + "*"
	}

	// stat 的格式串里用 | 分隔:文件名可能带空格,放在最后一段才切得开。
	// -print0 在这里用不上 —— 要的是 stat 的输出而不是纯路径
	cmd := fmt.Sprintf(
		"find %s -iname %s 2>/dev/null | head -n %d | xargs -d '\\n' -r stat -c '%%F|%%s|%%Y|%%n' 2>/dev/null",
		shellQuote(root), shellQuote(pattern), limit+1)

	out, stderr, err := s.run(cmd, searchTimeout)
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("在设备上执行查找失败: %w", err)
	}

	res := &SearchResult{Root: root, Pattern: pattern, Hits: []SearchHit{}, Stderr: strings.TrimSpace(stderr)}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		// 只切前 3 个 | ——  路径本身可能含 |
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		if len(res.Hits) >= limit {
			res.Truncated = true
			break
		}
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		mt, _ := strconv.ParseInt(parts[2], 10, 64)
		res.Hits = append(res.Hits, SearchHit{
			Path:    parts[3],
			IsDir:   parts[0] == "directory",
			Size:    size,
			ModTime: mt,
		})
	}
	return res, nil
}

// shellQuote 把一个值包成单引号字符串。
//
// 路径和模式都是用户输入的,直接拼进命令行等于把 shell 交给对方 ——
// 一个 `; rm -rf /` 就能在别人的设备上执行。单引号里除了单引号本身
// 什么都不特殊,所以只需要处理单引号:闭合、转义、再开。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var errNeedPattern = fmt.Errorf("要找什么?给个文件名或片段")
