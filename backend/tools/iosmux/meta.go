package iosmux

import (
	"bytes"
	"fmt"

	"howett.net/plist"
)

// ContainerMeta 每个应用容器里都有这么一份。
//
// iOS 上的容器目录名是一串 UUID,和包名毫无关系 —— 对应关系只写在这份 plist 里,
// 想知道哪个目录是微信、哪个是闲鱼,只能挨个打开来看
const ContainerMeta = ".com.apple.mobile_container_manager.metadata.plist"

// BundleIDFromMeta 从容器元信息里读出它属于哪个 App。
//
// 这是个二进制 plist,不能拿 grep 之类的去凑 —— 那样取出来的是碰巧
// 挨在一起的字符串,不保证是这个键的值
func BundleIDFromMeta(data []byte) (string, error) {
	var meta struct {
		Identifier string `plist:"MCMMetadataIdentifier"`
	}
	if _, err := plist.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("容器元信息读不懂: %w", err)
	}
	if meta.Identifier == "" {
		return "", fmt.Errorf("容器元信息里没有 MCMMetadataIdentifier")
	}
	return meta.Identifier, nil
}

// LooksLikePlist 粗判一段字节是不是 plist,省得把明显不对的东西送去解析
func LooksLikePlist(data []byte) bool {
	return bytes.HasPrefix(data, []byte("bplist00")) ||
		bytes.Contains(data[:min(len(data), 256)], []byte("<?xml"))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
