package adbx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// 内置 adb:让一台干净的电脑装完就能连设备,不用另外去下 platform-tools。
//
// 为什么要"解出来"而不是直接从内存里跑:Windows 上没法执行一段内存里的镜像,
// 而且 adb.exe 还要在同目录找到 AdbWinApi.dll / AdbWinUsbApi.dll。
// 所以第一次要用的时候解到 ~/.toolforge/platform-tools,之后直接复用。
//
// 只带三个文件(adb.exe + 两个 DLL,实测这三个就能跑起来),不是整包 ——
// 整包 17MB 里 fastboot、sqlite3、mke2fs 这些我们一个都用不上。
//
// 装载的载荷是构建时放进 bundled/ 的,代码不关心它从哪来:
// 官方 platform-tools 也好,自己从 AOSP 源码编的也好,换一份文件就行。

//go:embed bundled
var bundledFS embed.FS

const (
	// payloadName 载荷文件名。bundled/ 里没有它时,整套内置 adb 就是关掉的 ——
	// 代码照常编译、照常跑,只是回到"用 PATH 上的 adb"
	payloadName = "bundled/platform-tools.tar.gz"
	// stampName 记下已经解出来的是哪一份载荷。
	// 内容是载荷的哈希:载荷换了,下次自动重解,不用手工维护版本号
	stampName = ".bundled-adb"
	// maxMemberSize 单个成员的上限,防着一个坏掉的载荷把磁盘写满
	maxMemberSize = 64 << 20
)

// HasBundledPayload 这次构建里到底带没带 adb。
// 界面上要据此说人话:是"自带的"还是"得自己装一个"
func HasBundledPayload() bool {
	f, err := bundledFS.Open(payloadName)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// EnsureBundled 需要时把内置的 adb 解到 ~/.toolforge/platform-tools,返回 adb 的路径。
//
// 已经解过同一份载荷就直接返回,不重复写盘。
//
// 有一种情况故意不覆盖:那个目录里已经有 adb 但没有我们的标记 ——
// 那是用户自己放进去的,可能就是为了用某个特定版本,我们不该替他做主换掉。
func EnsureBundled() (string, error) {
	if !HasBundledPayload() {
		return "", errors.New("这个版本没有内置 adb")
	}
	dir, err := bundleDir()
	if err != nil {
		return "", err
	}
	exe := filepath.Join(dir, adbExeName())

	payload, err := bundledFS.ReadFile(payloadName)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])[:16]

	if got, err := os.ReadFile(filepath.Join(dir, stampName)); err == nil {
		if strings.TrimSpace(string(got)) == want {
			if st, err := os.Stat(exe); err == nil && !st.IsDir() {
				return exe, nil
			}
		}
	} else if st, err := os.Stat(exe); err == nil && !st.IsDir() {
		// 有 adb、没标记 = 用户自己放的,原样用他的
		return exe, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := extractTarGz(payload, dir); err != nil {
		return "", fmt.Errorf("解出内置 adb 失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, stampName), []byte(want), 0o644); err != nil {
		// 标记写不上不影响用,只是下次会白解一遍
		_ = err
	}
	if st, err := os.Stat(exe); err != nil || st.IsDir() {
		return "", fmt.Errorf("载荷里没有 %s", adbExeName())
	}
	return exe, nil
}

// bundleDir 解到哪儿。和手工安装用的是同一个位置,
// 这样"自己装过一份"的人升级上来什么都不用改
func bundleDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".toolforge", "platform-tools"), nil
}

// extractTarGz 解一个 tar.gz 到 dir。
//
// 载荷是我们自己构建时放进去的,不是从外面拿的,但校验照做 ——
// 一份被换掉的载荷不该有能力往目标目录外面写
func extractTarGz(payload []byte, dir string) error {
	zr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer zr.Close()

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// 只取文件名,连目录层级都不要 —— 三个文件必须躺在同一层,
		// adb.exe 是在自己旁边找那两个 DLL 的
		name := filepath.Base(filepath.FromSlash(hdr.Name))
		if name == "" || name == "." || name == string(filepath.Separator) {
			continue
		}
		if hdr.Size > maxMemberSize {
			return fmt.Errorf("%s 太大了(%d 字节)", name, hdr.Size)
		}
		target := filepath.Join(absDir, name)
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, maxMemberSize)); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
}

func adbExeName() string {
	if runtime.GOOS == "windows" {
		return "adb.exe"
	}
	return "adb"
}
