package devicefs

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"tool_forge/backend/tools/filehash"
	"tool_forge/backend/tools/mmkv"
	"tool_forge/backend/tools/plist"
)

// 预览是这个工具的重点。设备上翻到一个文件时,最耗时间的一步不是找到它,
// 是搞清楚它到底是什么 —— iOS 上一大半有价值的文件既没有扩展名,
// 打开也是一团二进制。
//
// 所以这里做的事是:拉一份到本地 → 认出类型 → 直接用对应的解析器解开。
// plist / MMKV 这两个解析器是现成的(前一轮刚从前端搬到 Go),
// 在这儿又派上一次用场 —— 这也是当时坚持搬过来的理由之一。

// Preview 一次预览的产物。
//
// 只有 Kind 对应的那个字段会填上,其余为空 —— 前端按 Kind 决定画哪个视图
type Preview struct {
	Path string `json:"path"`
	Name string `json:"name"`
	// Size 设备上的真实大小
	Size int64 `json:"size"`
	// Kind plist | mmkv | sqlite | image | text | other | binary
	Kind string `json:"kind"`
	// Why 为什么判成这个类型。判错时人得能看出是哪一步错了
	Why string `json:"why"`
	// LocalPath 拉到本地的那份副本,可以拿去做别的事
	LocalPath string `json:"localPath"`
	// Truncated 只拉了开头一段
	Truncated bool `json:"truncated"`

	Info  *filehash.FileInfo   `json:"info,omitempty"`
	Plist *plist.DesktopResult `json:"plist,omitempty"`
	MMKV  *mmkv.FileResult     `json:"mmkv,omitempty"`
	// Text 文本类文件的内容(已截断)
	Text string `json:"text,omitempty"`
	// ImageB64 图片的 data URI
	ImageB64 string `json:"imageB64,omitempty"`
	// Hex 认不出类型时的十六进制开头
	Hex string `json:"hex,omitempty"`
	// Note 解析没成功时的说明。认错类型不该表现成一个空白面板
	Note string `json:"note,omitempty"`
}

const (
	// previewPullLimit 预览最多从设备上拉多少字节。
	// 8MB 足够覆盖 plist / MMKV / 配置类文件;再大的多半是数据库或媒体,
	// 那种要看完整的应该走"导出"而不是预览
	previewPullLimit = 8 << 20
	// textPreviewLimit 文本最多显示多少字节
	textPreviewLimit = 256 << 10
	// imagePreviewLimit 超过这个大小就不内嵌成 data URI 了
	imagePreviewLimit = 4 << 20
	// hexPreviewLimit 认不出类型时给多少字节的十六进制
	hexPreviewLimit = 2 << 10
)

// Preview 拉一份下来并尽力认出它是什么
func (m *Manager) Preview(sessionID, remote, cacheDir string) (*Preview, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	remote = cleanRemote(remote)
	st, err := s.sftp.Stat(remote)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s 是个目录", remote)
	}

	local := filepath.Join(cacheDir, safeLocalName(remote))
	_, truncated, err := m.Pull(sessionID, remote, local, previewPullLimit)
	if err != nil {
		return nil, err
	}

	p := &Preview{
		Path:      remote,
		Name:      path.Base(remote),
		Size:      st.Size(),
		LocalPath: local,
		Truncated: truncated,
	}
	if info, err := filehash.InspectFile(local); err == nil {
		p.Info = info
	}
	head := readHead(local, 64)
	m.classify(p, s, remote, head)
	return p, nil
}

// classify 认类型并调对应的解析器。
//
// 顺序是按"证据强度"排的:有魔数的先认(bplist / SQLite / 图片),
// 没有魔数的靠旁证(MMKV 看有没有配套的 .crc),
// 都不沾边的再退到"能不能当文本读"。
func (m *Manager) classify(p *Preview, s *Session, remote string, head []byte) {
	switch {
	case len(head) >= 6 && string(head[:6]) == "bplist":
		p.Kind, p.Why = "plist", "文件头是 bplist"
		p.parsePlist()

	case looksLikeXMLPlist(head):
		p.Kind, p.Why = "plist", "文件头是 XML plist"
		p.parsePlist()

	case len(head) >= 15 && string(head[:15]) == "SQLite format 3":
		// 只认出来,不解析 —— 这个工具里没有 SQLite 浏览器。
		// 但把它认出来本身就有价值:iOS 上通讯录、短信、账号全在 sqlite 里,
		// 而它们的文件名经常看不出这一点
		p.Kind, p.Why = "sqlite", "文件头是 SQLite format 3"
		p.Note = "SQLite 数据库。这个工具没带表浏览器,先拉到本地再用别的工具打开"
		p.Hex = hexHead(p.LocalPath)

	case m.looksLikeMMKV(s, remote):
		p.Kind, p.Why = "mmkv", "旁边有同名的 .crc 文件,这是 MMKV 的落盘特征"
		p.parseMMKV()

	case p.Info != nil && renderableImage(p.Info.MimeType):
		p.Kind, p.Why = "image", "按魔数认出来是"+p.Info.MimeType
		p.loadImage()

	// 认得出是什么、但这里显示不了的:HEIC、视频、音频、PDF、压缩包。
	//
	// 这一支是拿真机数据试出来必须单列的:iPhone 的照片全是 HEIC,
	// 而 WebView 根本不认这个格式 —— 之前会当成图片内嵌进去,
	// 结果是一个碎掉的图片框,连"为什么看不了"都不说。
	// 与其给一屏十六进制,不如直接说清楚它是什么、下一步该怎么办。
	case p.Info != nil && knownButUnviewable(p.Info.Category):
		p.Kind = "other"
		p.Why = "按魔数认出来是" + p.Info.MimeType
		p.Note = unviewableHint(p.Info)
		p.Hex = hexHead(p.LocalPath)

	case isMostlyText(head):
		p.Kind, p.Why = "text", "开头是可读文本"
		p.loadText()

	default:
		p.Kind = "binary"
		p.Why = "没有认出已知的类型"
		p.Hex = hexHead(p.LocalPath)
		if p.Info != nil && p.Info.MagicHex != "" {
			p.Note = "魔数 " + p.Info.MagicHex
		}
	}
}

func (p *Preview) parsePlist() {
	res, err := plist.ParseFileForDesktop(p.LocalPath)
	if err != nil {
		// 截断的文件解不开是意料之中的:bplist 的偏移表在文件末尾,
		// 只拉了开头就一定读不了。这句话得说出来,不然人会以为文件坏了
		if p.Truncated {
			p.Note = "只拉了开头 8MB,而二进制 plist 的偏移表在文件末尾 —— " +
				"要看完整内容得先把整个文件导出来。" + err.Error()
		} else {
			p.Note = err.Error()
		}
		p.Hex = hexHead(p.LocalPath)
		return
	}
	p.Plist = res
}

func (p *Preview) parseMMKV() {
	res, err := mmkv.ParseFile(p.LocalPath, "", "")
	if err != nil {
		// 不在后面再补一句:ParseFile 已经分清了"空文件"和"可能加密"两种情况,
		// 一律追加"需要 AES key"会把空文件那条也说成加密的
		p.Note = err.Error()
		p.Hex = hexHead(p.LocalPath)
		return
	}
	p.MMKV = res
}

func (p *Preview) loadText() {
	data, err := os.ReadFile(p.LocalPath)
	if err != nil {
		p.Note = err.Error()
		return
	}
	if len(data) > textPreviewLimit {
		data = data[:textPreviewLimit]
		p.Truncated = true
	}
	// 截断可能正好切在一个多字节字符中间,末尾会多出个乱码字符
	p.Text = strings.ToValidUTF8(string(data), "")
}

func (p *Preview) loadImage() {
	st, err := os.Stat(p.LocalPath)
	if err != nil {
		p.Note = err.Error()
		return
	}
	if st.Size() > imagePreviewLimit {
		p.Note = fmt.Sprintf("图片 %.1f MB,超过内嵌预览的上限", float64(st.Size())/(1<<20))
		return
	}
	data, err := os.ReadFile(p.LocalPath)
	if err != nil {
		p.Note = err.Error()
		return
	}
	mime := "image/*"
	if p.Info != nil && p.Info.MimeType != "" {
		mime = p.Info.MimeType
	}
	p.ImageB64 = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// renderableImage WebView 真能画出来的图片格式。
//
// 不能拿"是不是图片"当条件:HEIC / HEIF / TIFF 都是正经图片格式,
// 但 Chromium 内核一个都不认,内嵌进去只会得到一个碎图框。
// 白名单而不是黑名单 —— 认错方向的代价不一样:漏掉一个格式只是少个预览,
// 多放进来一个就是一个不说话的碎图。
func renderableImage(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp",
		"image/bmp", "image/svg+xml", "image/x-icon", "image/avif":
		return true
	}
	return false
}

// knownButUnviewable 认得出是什么、但这里显示不了的大类
func knownButUnviewable(category string) bool {
	switch category {
	case "图片", "视频", "音频", "PDF", "压缩包":
		return true
	}
	return false
}

// unviewableHint 说清楚是什么、以及下一步该干什么。
// 光说"不支持预览"等于把人晾在那儿
func unviewableHint(info *filehash.FileInfo) string {
	switch info.Category {
	case "图片":
		return info.MimeType + " 这个格式浏览器画不出来(iPhone 的照片基本都是 HEIC)。" +
			"用「导出」拿到本地再看"
	case "视频", "音频":
		return info.Category + "(" + info.MimeType + ")这里放不了,用「导出」拿到本地再看"
	case "PDF":
		return "PDF 这里不展开,用「导出」拿到本地再看"
	case "压缩包":
		return "压缩包(" + info.MimeType + ")这里不解开,用「导出」拿到本地再看"
	}
	return info.MimeType + " 这里显示不了,用「导出」拿到本地再看"
}

// looksLikeMMKV MMKV 文件没有魔数 —— 开头就是个长度字段,和随便什么二进制没区别。
// 唯一可靠的旁证是它旁边有个同名的 .crc
func (m *Manager) looksLikeMMKV(s *Session, remote string) bool {
	_, err := s.sftp.Stat(remote + ".crc")
	return err == nil
}

func looksLikeXMLPlist(head []byte) bool {
	t := strings.TrimSpace(string(head))
	return strings.HasPrefix(t, "<?xml") || strings.HasPrefix(t, "<plist")
}

// isMostlyText 判断能不能当文本看。
// 要求是合法 UTF-8 且几乎没有 NUL —— 二进制文件里 NUL 遍地都是
func isMostlyText(head []byte) bool {
	if len(head) == 0 {
		return false
	}
	if !utf8.Valid(head) {
		return false
	}
	for _, b := range head {
		if b == 0 {
			return false
		}
	}
	return true
}

func readHead(p string, n int) []byte {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, n)
	got, _ := f.Read(buf)
	if got < 0 {
		got = 0
	}
	return buf[:got]
}

func hexHead(p string) string {
	b := readHead(p, hexPreviewLimit)
	return hex.EncodeToString(b)
}

// safeLocalName 把设备上的路径压成一个能当本地文件名的东西。
//
// 不能直接用 base name:两个不同目录下的同名文件会互相覆盖,
// 而 iOS 上到处都是叫 Info.plist / Cache.db 的文件。
// 把整条路径的分隔符换掉,既唯一又还能看出它是哪来的。
func safeLocalName(remote string) string {
	s := strings.TrimPrefix(remote, "/")
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	name := b.String()
	// Windows 的路径上限是 260;留出目录的余量
	const maxName = 150
	if len(name) > maxName {
		name = name[len(name)-maxName:]
	}
	if name == "" {
		name = "file"
	}
	return name
}
