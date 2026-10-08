package appsearch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

// 列表里显示的图标是图床现场缩出来的小图(Apple Store 512 JPG、七麦 iOS 180、应用宝 256),
// 保存和复制要的是原图:按图床把地址换成原图那个,换不了或者下不来,再用列表里那张。
//
// 下载一律在后端做:应用宝的图床不给跨域头,前端拿不到图片的字节。

// maxIconBytes 图标原图最大也就几百 KB,比这还大的不会是图标
const maxIconBytes = 10 << 20

var (
	errIconLocal    = errors.New("图标地址指向本机或内网,不去连")
	errIconNotImage = errors.New("下载到的不是图片")
	errIconTooBig   = errors.New("下载到的文件超过 10MB,不像是图标")
)

// IconRequest 前端点「保存」「复制」时传进来的
type IconRequest struct {
	Icon string `json:"icon"` // 列表里那张图的地址
	Name string `json:"name"` // 应用名,拼默认文件名用
	ID   string `json:"id"`   // 包名;没有包名时前端给 trackId
}

// IconInfo 实际拿到的那张图。列表里是缩略图,得让人知道存下来的到底多大
type IconInfo struct {
	Path   string `json:"path,omitempty"` // 存到哪了;复制时为空
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"` // png / jpeg / gif / webp
	Bytes  int    `json:"bytes"`
}

// icon 下载下来的原图
type icon struct {
	data          []byte
	format        string
	width, height int
}

func (ic *icon) info() IconInfo {
	return IconInfo{Width: ic.width, Height: ic.height, Format: ic.format, Bytes: len(ic.data)}
}

// iconExts 认得的图片格式和对应的扩展名
var iconExts = map[string]string{"png": ".png", "jpeg": ".jpg", "gif": ".gif", "webp": ".webp"}

var (
	mzstaticSizeRe = regexp.MustCompile(`/\d+x\d+[a-z]*(?:-\d+)?\.(?:jpe?g|png|webp)$`)
	trailingSizeRe = regexp.MustCompile(`/\d+$`)
)

// iconCandidates 列表里那张图的地址 → 依次要试的地址,原图在前,列表里那张垫底。
//
// 各图床的规矩(2026-10 实测):
//   - Apple(Apple Store 和七麦 iOS 用的都是它):末段的 512x512bb.jpg 是让图床现场缩放,
//     要 1024x1024bb.png 给的就是上传的原图;要 2048 也只回 1024
//   - 应用宝:末段数字是边长,/0 是原图(512 或 256,看开发者传的);
//     没有 /512、/1024 这种档,要了回 400
//   - Google Play:不带 =s64 之类的后缀就是原图(512),要更大也不给
func iconCandidates(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil {
		return []string{raw}
	}
	host := strings.ToLower(u.Hostname())
	best := ""
	switch {
	case strings.HasSuffix(host, ".mzstatic.com"):
		best = mzstaticSizeRe.ReplaceAllString(raw, "/1024x1024bb.png")
	case host == "pp.myapp.com":
		best = trailingSizeRe.ReplaceAllString(raw, "/0")
	case strings.HasSuffix(host, ".googleusercontent.com"):
		if i := strings.LastIndex(raw, "="); i > strings.LastIndex(raw, "/") {
			best = raw[:i]
		}
	}
	if best == "" || best == raw {
		return []string{raw}
	}
	return []string{best, raw}
}

// newIconClient 下图标用的客户端。跳转也要过一遍 checkIconHost
func newIconClient(rt http.RoundTripper) *http.Client {
	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: rt,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("跳转次数太多")
			}
			return checkIconHost(req.URL)
		},
	}
}

// checkIconHost 图标地址是第三方接口给的,不拿它去连本机和内网。
//
// 只看地址里写的主机,不看域名解析出来的 IP:开着 TUN(fake-ip)时,
// 公网域名也会解析到保留网段,按解析结果拦会把正常的图标一起拦掉
func checkIconHost(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("图标地址不对: %s", u.Redacted())
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return errIconLocal
		}
		return nil
	}
	// 没有点的单段主机名(router 之类)只在局域网里解析得到
	if host == "" || host == "localhost" || !strings.Contains(host, ".") ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return errIconLocal
	}
	return nil
}

// fetchIcon 下载图标原图:原图地址下不来就退回列表里那张
func (s *Service) fetchIcon(ctx context.Context, raw string) (*icon, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("图标地址不对: %q", raw)
	}
	if err := checkIconHost(u); err != nil {
		return nil, err
	}
	var lastErr error
	for _, c := range iconCandidates(raw) {
		ic, err := downloadImage(ctx, s.icons, c)
		if err == nil {
			return ic, nil
		}
		// 报给用户的是列表里那张为什么下不来:原图地址失败是常事,说了也没用
		lastErr = err
	}
	return nil, lastErr
}

func downloadImage(ctx context.Context, client *http.Client, u string) (*icon, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("图标地址不对: %w", err)
	}
	req.Header.Set("User-Agent", defaultUA)
	resp, err := client.Do(req)
	if err != nil {
		return nil, describeNetErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载图标失败: http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIconBytes+1))
	if err != nil {
		return nil, describeNetErr(err)
	}
	if len(data) > maxIconBytes {
		return nil, errIconTooBig
	}
	// 格式看文件头,不信 Content-Type:图床出错时也可能回 200 加一个网页
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || iconExts[format] == "" {
		return nil, errIconNotImage
	}
	return &icon{data: data, format: format, width: cfg.Width, height: cfg.Height}, nil
}

// describeNetErr Go 的网络错误带着整条地址,界面上那一行放不下,压成一句
func describeNetErr(err error) error {
	if errors.Is(err, errIconLocal) {
		return errIconLocal
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return errors.New("下载图标超时")
		}
		err = ue.Err
	}
	return fmt.Errorf("下载图标失败: %w", err)
}

// SaveIcon 先把原图下下来,再让用户选存到哪。
// pick 弹保存框,返回空串表示用户取消了,这时返回 nil, nil。
//
// 顺序不能反:扩展名要按实际下到的格式定;下载失败也该在弹框之前说,
// 而不是让人先选好位置再报错
func (s *Service) SaveIcon(ctx context.Context, req IconRequest, pick func(defaultName, ext string) (string, error)) (*IconInfo, error) {
	ic, err := s.fetchIcon(ctx, req.Icon)
	if err != nil {
		return nil, err
	}
	ext := iconExts[ic.format]
	path, err := pick(iconFileName(req.Name, req.ID, ext), ext)
	if err != nil || path == "" {
		return nil, err
	}
	path = withImageExt(path, ext)
	if err := os.WriteFile(path, ic.data, 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}
	info := ic.info()
	info.Path = path
	return &info, nil
}

// IconPNG 下原图并转成 PNG,复制到剪贴板用
func (s *Service) IconPNG(ctx context.Context, raw string) ([]byte, *IconInfo, error) {
	ic, err := s.fetchIcon(ctx, raw)
	if err != nil {
		return nil, nil, err
	}
	data := ic.data
	if ic.format != "png" {
		img, _, err := image.Decode(bytes.NewReader(ic.data))
		if err != nil {
			return nil, nil, errIconNotImage
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, nil, fmt.Errorf("转成 PNG 失败: %w", err)
		}
		data = buf.Bytes()
	}
	info := ic.info()
	info.Format, info.Bytes = "png", len(data)
	return data, &info, nil
}

// iconFileName 保存框里的默认文件名:「应用名_包名」加扩展名,比如 微信_com.tencent.xin.png
func iconFileName(name, id, ext string) string {
	var parts []string
	if n := cleanFileName(name, 60); n != "" {
		parts = append(parts, n)
	}
	if i := cleanFileName(id, 120); i != "" {
		parts = append(parts, i)
	}
	base := strings.Join(parts, "_")
	if base == "" {
		base = "icon"
	}
	// CON、NUL 这些是 Windows 的设备名,不能拿来当文件名
	if winDeviceNames[strings.ToUpper(strings.SplitN(base, ".", 2)[0])] {
		base += "_icon"
	}
	return base + ext
}

var winDeviceNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// cleanFileName 换掉文件名里不能有的字符,太长的截断
func cleanFileName(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return ' '
		case strings.ContainsRune(`\/:*?"<>|`, r):
			return '_'
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	// 结尾的点和空格 Windows 会直接吃掉
	return strings.Trim(s, " .")
}

// withImageExt 用户在保存框里把扩展名删了,就补上实际格式的;改成别的图片扩展名的,听用户的。
//
// 不能只看有没有扩展名:默认名里的包名自带点,「微信_com.tencent.xin」的扩展名会被认成 .xin
func withImageExt(path, ext string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return path
	}
	return path + ext
}
