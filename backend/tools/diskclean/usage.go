package diskclean

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// 空间占用:按目录看每一层各占多少,一层层点进去找"C 盘被谁吃了"。
//
// 目录树是在大文件扫描那一遍里顺带记下来的 —— 已经把每个目录都走过一遍了,
// 再为它单独扫一次纯属浪费。树留在内存里,点进哪一层就当场从树上取,不再碰磁盘。
// 只留最近一次扫描的那一棵:下一次扫描开始就换掉

// usageNode 树上的一个目录。只存名字不存完整路径:
// 全盘几十万个目录,每个都存一份完整路径要多占几十 MB,路径查询时现拼就行
type usageNode struct {
	name     string
	parent   *usageNode
	children []*usageNode
	self     int64 // 直接放在这一层的文件
	selfN    int64
	size     int64 // 连同子目录
	files    int64
	// partial 它或者它下面有读不全的目录,实际可能比这个数大
	partial bool
}

type usageTree struct {
	id    string
	roots []*usageNode // 根节点的 name 存的是完整路径
}

// usageBuilder 扫描时收集每个目录这一层的统计,扫完再拼成树
type usageBuilder struct {
	mu   sync.Mutex
	recs []usageRec
}

type usageRec struct {
	path       string
	bytes      int64
	files      int64
	incomplete bool
}

func (b *usageBuilder) add(dir string, st dirStat) {
	b.mu.Lock()
	b.recs = append(b.recs, usageRec{dir, st.bytes, st.files, st.incomplete})
	b.mu.Unlock()
}

func (b *usageBuilder) build(id string, roots []string) *usageTree {
	b.mu.Lock()
	defer b.mu.Unlock()
	// 拼树用一张临时的 路径 → 节点 表,拼完就扔。
	// 路径都是遍历时 Join 出来的,父目录用 filepath.Dir 一定能原样对上
	byPath := make(map[string]*usageNode, len(b.recs))
	for _, r := range b.recs {
		byPath[r.path] = &usageNode{name: filepath.Base(r.path), self: r.bytes, selfN: r.files, partial: r.incomplete}
	}
	t := &usageTree{id: id}
	isRoot := map[string]bool{}
	for _, r := range roots {
		isRoot[r] = true
	}
	for _, r := range b.recs {
		n := byPath[r.path]
		if isRoot[r.path] {
			n.name = r.path
			t.roots = append(t.roots, n)
			continue
		}
		// 父目录没有记录的(扫描中途取消了)就挂不上,丢掉 —— 结果会标成不完整
		if p := byPath[filepath.Dir(r.path)]; p != nil {
			n.parent = p
			p.children = append(p.children, n)
		}
	}
	// 从深到浅把大小往上加:子目录的路径一定比父目录长
	order := make([]usageRec, len(b.recs))
	copy(order, b.recs)
	sort.Slice(order, func(i, j int) bool { return len(order[i].path) > len(order[j].path) })
	for _, r := range order {
		n := byPath[r.path]
		n.size += n.self
		n.files += n.selfN
		if p := n.parent; p != nil {
			p.size += n.size
			p.files += n.files
			p.partial = p.partial || n.partial
		}
	}
	sort.Slice(t.roots, func(i, j int) bool { return t.roots[i].name < t.roots[j].name })
	b.recs = nil
	return t
}

// find 按路径找节点。逐段按名字比(不区分大小写),不靠字符串前缀 ——
// 大小写转换后字节长度可能变,按长度切前缀会切错
func (t *usageTree) find(dir string) *usageNode {
	want := splitPath(dir)
	for _, r := range t.roots {
		have := splitPath(r.name)
		if len(want) < len(have) || !samePrefix(want, have) {
			continue
		}
		n := r
		for _, part := range want[len(have):] {
			var next *usageNode
			for _, c := range n.children {
				if strings.EqualFold(c.name, part) {
					next = c
					break
				}
			}
			if next == nil {
				return nil
			}
			n = next
		}
		return n
	}
	return nil
}

// splitPath 拆成 [卷, 第一段, 第二段, ...]
func splitPath(p string) []string {
	p = filepath.Clean(p)
	vol := filepath.VolumeName(p)
	out := []string{vol}
	for _, s := range strings.Split(strings.Trim(p[len(vol):], string(filepath.Separator)), string(filepath.Separator)) {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func samePrefix(a, prefix []string) bool {
	for i := range prefix {
		if !strings.EqualFold(a[i], prefix[i]) {
			return false
		}
	}
	return true
}

// UsageEntry 一层里的一项
type UsageEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Files int64  `json:"files"`
	// IsDir 为假时这一项是"直接放在这一层的文件"的合计
	IsDir   bool `json:"isDir"`
	Partial bool `json:"partial"`
	// Note 受保护目录的说明(比如 WinSxS 为什么看着大),和上一层说法一样的不重复
	Note string `json:"note,omitempty"`
}

// UsageLevel 某一层的占用
type UsageLevel struct {
	// Path 空表示最顶层:列的是扫描的那几个起点
	Path    string       `json:"path"`
	Size    int64        `json:"size"`
	Files   int64        `json:"files"`
	Partial bool         `json:"partial"`
	Entries []UsageEntry `json:"entries"`
	// More / MoreSize 太多没单独列出来的
	More     int   `json:"more"`
	MoreSize int64 `json:"moreSize"`
}

// maxUsageEntries 一层最多列多少项。再多的人也看不过来,合成一行"其余"
const maxUsageEntries = 200

var errUsageGone = errors.New("这份目录统计已经过期了,重新扫一遍")

// UsageChildren 某一层的占用,按大小从大到小
func (s *Service) UsageChildren(id, dir string) (*UsageLevel, error) {
	s.usageMu.Lock()
	t := s.usage
	s.usageMu.Unlock()
	if t == nil || t.id != id {
		return nil, errUsageGone
	}

	lv := &UsageLevel{Path: dir, Entries: []UsageEntry{}}
	if dir == "" {
		for _, r := range t.roots {
			lv.Size += r.size
			lv.Files += r.files
			lv.Partial = lv.Partial || r.partial
			lv.Entries = append(lv.Entries, UsageEntry{
				Name: r.name, Path: r.name, Size: r.size, Files: r.files, IsDir: true, Partial: r.partial,
			})
		}
	} else {
		n := t.find(dir)
		if n == nil {
			return nil, errors.New("这个目录不在扫描结果里")
		}
		lv.Size, lv.Files, lv.Partial = n.size, n.files, n.partial
		parentNote := ""
		if v := s.guard.Check(dir); v.Blocked {
			parentNote = v.Reason
		}
		for _, c := range n.children {
			p := filepath.Join(dir, c.name)
			e := UsageEntry{Name: c.name, Path: p, Size: c.size, Files: c.files, IsDir: true, Partial: c.partial}
			if v := s.guard.Check(p); v.Blocked && v.Reason != parentNote {
				e.Note = v.Reason
			}
			lv.Entries = append(lv.Entries, e)
		}
		if n.selfN > 0 {
			lv.Entries = append(lv.Entries, UsageEntry{
				Name: "直接放在这里的文件", Path: dir, Size: n.self, Files: n.selfN,
			})
		}
	}
	sort.SliceStable(lv.Entries, func(i, j int) bool { return lv.Entries[i].Size > lv.Entries[j].Size })
	if len(lv.Entries) > maxUsageEntries {
		for _, e := range lv.Entries[maxUsageEntries:] {
			lv.More++
			lv.MoreSize += e.Size
		}
		lv.Entries = lv.Entries[:maxUsageEntries]
	}
	return lv, nil
}
