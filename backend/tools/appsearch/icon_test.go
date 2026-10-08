package appsearch

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const (
	mzThumb  = "https://is1-ssl.mzstatic.com/image/thumb/Purple221/v4/61/70/2e/demo/AppIcon-0-0-1x_U007epad-0-1-0-sRGB-0-85-220.png/512x512bb.jpg"
	mzOrig   = "https://is1-ssl.mzstatic.com/image/thumb/Purple221/v4/61/70/2e/demo/AppIcon-0-0-1x_U007epad-0-1-0-sRGB-0-85-220.png/1024x1024bb.png"
	yybThumb = "http://pp.myapp.com/ma_icon/0/icon_10910_1789722197/256"
	yybOrig  = "http://pp.myapp.com/ma_icon/0/icon_10910_1789722197/0"
)

func TestIconCandidates(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"Apple 列表里的 512 JPG", mzThumb, []string{mzOrig, mzThumb}},
		{"七麦 iOS 的 180 PNG", strings.Replace(mzThumb, "512x512bb.jpg", "180x180bb.png", 1), []string{mzOrig, strings.Replace(mzThumb, "512x512bb.jpg", "180x180bb.png", 1)}},
		{"Apple 带质量后缀", strings.Replace(mzThumb, "512x512bb.jpg", "100x100bb-85.png", 1), []string{mzOrig, strings.Replace(mzThumb, "512x512bb.jpg", "100x100bb-85.png", 1)}},
		{"已经是原图就不重复试", mzOrig, []string{mzOrig}},
		{"Apple 末段不是尺寸的不动", "https://is1-ssl.mzstatic.com/image/thumb/demo/icon.png", []string{"https://is1-ssl.mzstatic.com/image/thumb/demo/icon.png"}},
		{"应用宝 /256 换 /0", yybThumb, []string{yybOrig, yybThumb}},
		{"应用宝已经是 /0", yybOrig, []string{yybOrig}},
		{"Google 去掉尺寸后缀", "https://play-lh.googleusercontent.com/AbC_d-Ef=s64-rw", []string{"https://play-lh.googleusercontent.com/AbC_d-Ef", "https://play-lh.googleusercontent.com/AbC_d-Ef=s64-rw"}},
		{"Google 本来就没后缀", "https://play-lh.googleusercontent.com/AbC_d-Ef", []string{"https://play-lh.googleusercontent.com/AbC_d-Ef"}},
		{"不认识的图床原样用", "https://pic.example.com/icon/a.png?x=1", []string{"https://pic.example.com/icon/a.png?x=1"}},
	}
	for _, c := range cases {
		if got := iconCandidates(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got  %v\n want %v", c.name, got, c.want)
		}
	}
}

func TestCheckIconHost(t *testing.T) {
	ok := []string{mzThumb, yybThumb, "https://play-lh.googleusercontent.com/x", "https://8.8.8.8/x.png"}
	for _, s := range ok {
		if err := checkIconHost(mustURL(t, s)); err != nil {
			t.Errorf("%s 被拦了: %v", s, err)
		}
	}
	local := []string{
		"http://127.0.0.1:8080/x.png", "http://[::1]/x.png", "http://localhost/x.png", "http://app.localhost/x.png",
		"http://10.0.0.5/x.png", "http://192.168.1.1/x.png", "http://172.16.0.1/x.png", "http://169.254.169.254/x.png",
		"http://router/x.png", "http://nas.local/x.png", "http://0.0.0.0/x.png",
	}
	for _, s := range local {
		if err := checkIconHost(mustURL(t, s)); !errors.Is(err, errIconLocal) {
			t.Errorf("%s 应该拦下,得到 %v", s, err)
		}
	}
	if err := checkIconHost(mustURL(t, "ftp://example.com/x.png")); err == nil {
		t.Error("ftp 地址应该不认")
	}
}

func TestFetchIconPrefersOriginal(t *testing.T) {
	fake := newFakeIcons(map[string]fakeResp{
		mzOrig:  {status: 200, body: pngBytes(t, 16, 16)},
		mzThumb: {status: 200, body: jpegBytes(t, 8, 8)},
	})
	ic, err := (&Service{icons: newIconClient(fake)}).fetchIcon(context.Background(), mzThumb)
	if err != nil {
		t.Fatal(err)
	}
	if ic.format != "png" || ic.width != 16 {
		t.Fatalf("拿到的应该是原图 16x16 PNG,实际 %s %dx%d", ic.format, ic.width, ic.height)
	}
	if got := fake.requested(); !reflect.DeepEqual(got, []string{mzOrig}) {
		t.Fatalf("原图下到了就不该再要缩略图: %v", got)
	}
}

func TestFetchIconFallsBackToListed(t *testing.T) {
	fake := newFakeIcons(map[string]fakeResp{
		yybOrig:  {status: 400},
		yybThumb: {status: 200, body: pngBytes(t, 4, 4)},
	})
	ic, err := (&Service{icons: newIconClient(fake)}).fetchIcon(context.Background(), yybThumb)
	if err != nil {
		t.Fatal(err)
	}
	if ic.width != 4 {
		t.Fatalf("原图下不来要退回列表里那张,实际 %dx%d", ic.width, ic.height)
	}
	if got := fake.requested(); !reflect.DeepEqual(got, []string{yybOrig, yybThumb}) {
		t.Fatalf("顺序不对: %v", got)
	}
}

func TestFetchIconRejectsNonImage(t *testing.T) {
	page := []byte("<html><body>出错了</body></html>")
	fake := newFakeIcons(map[string]fakeResp{
		mzOrig:  {status: 200, body: page},
		mzThumb: {status: 200, body: page},
	})
	_, err := (&Service{icons: newIconClient(fake)}).fetchIcon(context.Background(), mzThumb)
	if !errors.Is(err, errIconNotImage) {
		t.Fatalf("回 200 加网页不能当成图标,得到 %v", err)
	}
}

func TestFetchIconTooBig(t *testing.T) {
	huge := append(pngBytes(t, 4, 4), make([]byte, maxIconBytes)...)
	fake := newFakeIcons(map[string]fakeResp{yybThumb: {status: 200, body: huge}})
	_, err := (&Service{icons: newIconClient(fake)}).fetchIcon(context.Background(), yybThumb)
	if !errors.Is(err, errIconTooBig) {
		t.Fatalf("超过 10MB 应该拒收,得到 %v", err)
	}
}

func TestFetchIconRefusesLocal(t *testing.T) {
	fake := newFakeIcons(nil)
	_, err := (&Service{icons: newIconClient(fake)}).fetchIcon(context.Background(), "http://127.0.0.1:9/icon.png")
	if !errors.Is(err, errIconLocal) {
		t.Fatalf("本机地址应该拦下,得到 %v", err)
	}
	if got := fake.requested(); len(got) != 0 {
		t.Fatalf("拦下的地址不该发出任何请求: %v", got)
	}
}

// 地址本身是公网的,跳转过去的是内网:跳转那一下也得拦住
func TestFetchIconRefusesRedirectToLocal(t *testing.T) {
	fake := newFakeIcons(map[string]fakeResp{
		yybOrig:  {status: 302, location: "http://192.168.1.1/x.png"},
		yybThumb: {status: 302, location: "http://192.168.1.1/x.png"},
	})
	_, err := (&Service{icons: newIconClient(fake)}).fetchIcon(context.Background(), yybThumb)
	if !errors.Is(err, errIconLocal) {
		t.Fatalf("跳到内网应该拦下,得到 %v", err)
	}
	for _, u := range fake.requested() {
		if strings.Contains(u, "192.168.") {
			t.Fatalf("内网地址被请求了: %v", fake.requested())
		}
	}
}

func TestSaveIcon(t *testing.T) {
	orig := pngBytes(t, 16, 16)
	fake := newFakeIcons(map[string]fakeResp{mzOrig: {status: 200, body: orig}})
	svc := &Service{icons: newIconClient(fake)}
	dir := t.TempDir()

	// 用户把扩展名删了:补回实际格式的
	var gotName, gotExt string
	info, err := svc.SaveIcon(context.Background(), IconRequest{Icon: mzThumb, Name: "微信", ID: "com.tencent.xin"},
		func(name, ext string) (string, error) {
			gotName, gotExt = name, ext
			return filepath.Join(dir, "微信_com.tencent.xin"), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "微信_com.tencent.xin.png" || gotExt != ".png" {
		t.Fatalf("默认文件名不对: %q %q", gotName, gotExt)
	}
	want := filepath.Join(dir, "微信_com.tencent.xin.png")
	if info.Path != want || info.Width != 16 || info.Format != "png" || info.Bytes != len(orig) {
		t.Fatalf("返回的信息不对: %+v", info)
	}
	if data, err := os.ReadFile(want); err != nil || !bytes.Equal(data, orig) {
		t.Fatalf("存下来的应该就是下到的原图字节, err=%v", err)
	}

	// 在保存框里点了取消:不报错,不写文件
	info, err = svc.SaveIcon(context.Background(), IconRequest{Icon: mzThumb, Name: "取消"},
		func(string, string) (string, error) { return "", nil })
	if info != nil || err != nil {
		t.Fatalf("取消应该返回 nil, nil,得到 %+v %v", info, err)
	}

	// 下载失败:保存框根本不该弹出来
	picked := false
	_, err = svc.SaveIcon(context.Background(), IconRequest{Icon: "http://localhost/x.png"},
		func(string, string) (string, error) { picked = true; return "", nil })
	if err == nil || picked {
		t.Fatalf("下载失败时不该弹保存框: picked=%v err=%v", picked, err)
	}
}

func TestIconPNGConvertsJPEG(t *testing.T) {
	fake := newFakeIcons(map[string]fakeResp{
		mzOrig:  {status: 404},
		mzThumb: {status: 200, body: jpegBytes(t, 12, 10)},
	})
	data, info, err := (&Service{icons: newIconClient(fake)}).IconPNG(context.Background(), mzThumb)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("复制用的应该是 PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 12 || b.Dy() != 10 {
		t.Fatalf("转格式不该改尺寸: %v", b)
	}
	if info.Format != "png" || info.Width != 12 || info.Bytes != len(data) {
		t.Fatalf("返回的信息不对: %+v", info)
	}
}

func TestIconFileName(t *testing.T) {
	cases := []struct{ name, id, want string }{
		{"微信", "com.tencent.xin", "微信_com.tencent.xin.png"},
		{"元宝-腾讯旗下AI助手", "com.tencent.hunyuan.app.chat", "元宝-腾讯旗下AI助手_com.tencent.hunyuan.app.chat.png"},
		{`A/B:C*D?"E<F>G|H\I`, "x.y", "A_B_C_D__E_F_G_H_I_x.y.png"},
		{"  多余   的\t空白 ", "", "多余 的 空白.png"},
		{"结尾带点...", "", "结尾带点.png"},
		{"", "414478124", "414478124.png"},
		{"", "", "icon.png"},
		{"con", "", "con_icon.png"},
		{"CON", "com.x", "CON_com.x.png"},
		{strings.Repeat("长", 80), "", strings.Repeat("长", 60) + ".png"},
	}
	for _, c := range cases {
		if got := iconFileName(c.name, c.id, ".png"); got != c.want {
			t.Errorf("iconFileName(%q, %q) = %q,应为 %q", c.name, c.id, got, c.want)
		}
	}
}

func TestWithImageExt(t *testing.T) {
	cases := []struct{ in, want string }{
		{`D:\图标\微信_com.tencent.xin`, `D:\图标\微信_com.tencent.xin.png`},
		{`D:\图标\微信.png`, `D:\图标\微信.png`},
		{`D:\图标\微信.JPG`, `D:\图标\微信.JPG`},
		{`D:\图标\微信`, `D:\图标\微信.png`},
	}
	for _, c := range cases {
		if got := withImageExt(c.in, ".png"); got != c.want {
			t.Errorf("withImageExt(%q) = %q,应为 %q", c.in, got, c.want)
		}
	}
}

// ---- 假图床:按完整地址回预设的响应,记下请求过哪些地址 ----

type fakeResp struct {
	status   int
	body     []byte
	location string
}

type fakeIcons struct {
	mu   sync.Mutex
	resp map[string]fakeResp
	got  []string
}

func newFakeIcons(resp map[string]fakeResp) *fakeIcons {
	return &fakeIcons{resp: resp}
}

func (f *fakeIcons) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	f.mu.Lock()
	f.got = append(f.got, u)
	r, ok := f.resp[u]
	f.mu.Unlock()
	if !ok {
		r = fakeResp{status: 404}
	}
	h := http.Header{}
	if r.location != "" {
		h.Set("Location", r.location)
	}
	return &http.Response{
		StatusCode: r.status,
		Header:     h,
		Body:       io.NopCloser(bytes.NewReader(r.body)),
		Request:    req,
	}, nil
}

func (f *fakeIcons) requested() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
