package mirror

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electricbubble/gadb"

	"tool_forge/backend/tools/adbx"
)

// minSDK 手机端程序最低要安卓 5.0(API 21)
const minSDK = 21

// deviceInfo 启动前从手机上读的几项属性
type deviceInfo struct {
	serial string
	// sdk API 级别,读不到时是 0
	sdk     int
	release string
	// brand 品牌和厂商连在一起、转小写,认机型给提示用
	brand string
}

// prepareDevice 选中设备、读几项属性,把手机端程序推上去。
// 设备没授权、不在线这类情况,adbx 已经会说人话
func prepareDevice(adbPath, serial string) (deviceInfo, error) {
	client, err := adbx.Dial(adbPath)
	if err != nil {
		return deviceInfo{}, err
	}
	dev, _, err := adbx.PickDevice(client, serial)
	if err != nil {
		return deviceInfo{}, err
	}
	info := readDeviceInfo(dev)
	// 老系统上手机端程序根本起不来,不先拦下的话要干等 10 秒,最后报一句看不出原因的「连不上」
	if info.sdk > 0 && info.sdk < minSDK {
		return info, fmt.Errorf("这台手机是安卓 %s，投屏要安卓 5.0 以上", info.release)
	}
	if err := dev.Push(bytes.NewReader(serverJar), devicePath, time.Now(), 0o644); err != nil {
		return info, fmt.Errorf("往手机上推投屏程序失败: %w", err)
	}
	return info, nil
}

// readDeviceInfo 一次 shell 把要的属性都读回来。读不到就留空,不耽误投屏
func readDeviceInfo(dev gadb.Device) deviceInfo {
	out, _ := adbx.Text(dev, "getprop ro.build.version.sdk; getprop ro.build.version.release; "+
		"getprop ro.product.brand; getprop ro.product.manufacturer", false, 10*time.Second)
	return parseDeviceInfo(dev.Serial(), out)
}

// parseDeviceInfo 四行输出依次是 API 级别、系统版本、品牌、厂商
func parseDeviceInfo(serial, out string) deviceInfo {
	lines := strings.Split(strings.ReplaceAll(out, "\r", ""), "\n")
	line := func(i int) string {
		if i < len(lines) {
			return strings.TrimSpace(lines[i])
		}
		return ""
	}
	sdk, _ := strconv.Atoi(line(0))
	return deviceInfo{
		serial:  serial,
		sdk:     sdk,
		release: line(1),
		brand:   strings.ToLower(strings.TrimSpace(line(2) + " " + line(3))),
	}
}

// injectDeniedNotice 手机不让模拟点击时的提醒。各家的开关不一样,认得出品牌就直接说是哪个
func injectDeniedNotice(brand string) notice {
	has := func(names ...string) bool {
		for _, n := range names {
			if strings.Contains(brand, n) {
				return true
			}
		}
		return false
	}
	var text string
	switch {
	case has("xiaomi", "redmi", "poco"):
		text = "手机拒绝了模拟点击。小米、红米要在「开发者选项」里再打开「USB 调试（安全设置）」，打开后重启手机再试"
	case has("oppo", "oneplus", "realme"):
		text = "手机拒绝了模拟点击。OPPO、一加、真我要在「开发者选项」里打开「禁止权限监控」" +
			"（有的系统版本叫「权限监控」，要关掉），然后重新连接投屏"
	default:
		text = "手机拒绝了模拟点击。到手机的「开发者选项」里找带「模拟点击」「模拟输入」或「安全设置」字样的开关打开，" +
			"然后重新连接投屏。小米、红米是「USB 调试（安全设置）」，OPPO、一加、真我是「禁止权限监控」"
	}
	return notice{Type: "notice", Code: "inject-denied", Text: text}
}
