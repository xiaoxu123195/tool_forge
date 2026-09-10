package mmkv

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// 加密的 MMKV:
//   - 文件头 4 字节(size header)是明文
//   - 剩下的用 AES-128-CFB 加密
//   - IV 不在这个文件里,而在配对的 .crc 文件的第 [12:28] 字节
//   - key 是十六进制字符串,不足 16 字节补 0x00,超了截断
//
// IV 在另一个文件里这件事是这套格式最容易卡住人的地方 —— 只拿到 .mmkv
// 是解不开的,必须连 .crc 一起提出来。

// crcIVOffset / crcIVLength IV 在 .crc 文件里的位置
const (
	crcIVOffset = 12
	crcIVLength = 16
)

// Decrypt 用 .crc 里的 IV 和给定的 key 解密一个 MMKV 文件,
// 返回可以直接交给 Parse 的明文(头 4 字节原样保留)。
func Decrypt(encrypted, crc []byte, keyHex string) ([]byte, error) {
	if len(encrypted) < 4 {
		return nil, errors.New("加密的 MMKV 文件过小(不足 4 字节)")
	}
	if len(crc) < crcIVOffset+crcIVLength {
		return nil, fmt.Errorf("配套的 .crc 文件过小(%d 字节,至少要 %d)——"+
			"IV 存在 .crc 里,只有 .mmkv 是解不开的",
			len(crc), crcIVOffset+crcIVLength)
	}
	key, err := parseKeyHex(keyHex)
	if err != nil {
		return nil, err
	}
	iv := crc[crcIVOffset : crcIVOffset+crcIVLength]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	body := encrypted[4:]
	plain := make([]byte, len(body))
	// CFB 全块反馈(CFB-128),和前端 CryptoJS 的默认模式一致 ——
	// 换成 CFB-8 解出来是一堆乱码,而且不会报错,只会"看起来没解开"
	cipher.NewCFBDecrypter(block, iv).XORKeyStream(plain, body)

	out := make([]byte, 4+len(plain))
	copy(out, encrypted[:4])
	copy(out[4:], plain)
	return out, nil
}

// parseKeyHex 十六进制 key → 16 字节。短的补零,长的截断 ——
// 和 MMKV 自己的做法一致,不是我们在放宽要求
func parseKeyHex(s string) ([]byte, error) {
	cleaned := strings.Join(strings.Fields(s), "")
	if cleaned == "" {
		return nil, errors.New("AES key 不能为空")
	}
	if len(cleaned)%2 != 0 {
		return nil, errors.New("AES key 的十六进制字符数应该是偶数")
	}
	raw, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, errors.New("AES key 应该是十六进制字符串")
	}
	key := make([]byte, 16)
	copy(key, raw) // 短则补零,长则截断
	return key, nil
}
