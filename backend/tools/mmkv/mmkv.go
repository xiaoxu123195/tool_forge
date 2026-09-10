// Package mmkv 解析腾讯 MMKV 的存储文件。
//
// 这份实现是从前端那份 TypeScript 移过来的。搬到 Go 的直接原因是 MCP:
// agent 手上拿到一个从设备里提出来的 MMKV 文件时,除了看一堆十六进制没有别的办法 ——
// 而 MCP 服务在 Go 里,调不到浏览器里的解析器。
package mmkv

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// MMKV 文件格式(Tencent MMKV,未加密时):
//
//	[4B 小端 uint32 dbSize]
//	[varint 用途不明]        // 官方源码没写;实测常见 0xffffff07
//	循环:
//	  [varint keyLen] [utf8 key] [varint valLen] [raw value]
//
// valLen == 0 是删除标记 —— 键还留在日志里,但代表这个键已被移除。
// 这一点对取证有用:能看出"曾经存过什么又被删了"。

// Entry 一个键,以及它在追加日志里留下的历史值(新的在前)
type Entry struct {
	Key string `json:"key"`
	// Values 同一个键被写过多次时,每次的值都还在文件里。
	// 新的在前 —— 当前生效的是第一个,后面的是历史残留
	Values []Value `json:"values"`
}

// ParseResult 一次解析的产物
type ParseResult struct {
	Entries []Entry `json:"entries"`
	// DBSize 文件头声明的大小
	DBSize int `json:"dbSize"`
	// Consumed 实际消费掉的字节数;和 DBSize 差太多说明文件可能被截断或有填充
	Consumed int `json:"consumed"`
	// RemovedCount 带删除标记的键数
	RemovedCount int `json:"removedCount"`
}

// readVarintU32 读一个 32 位无符号 varint,最多 5 字节有效位;超出的位按 32 位截断。
//
// 截断而不是报错,是跟前端那份实现对齐的行为 —— MMKV 里确实存在
// 用 64 位 varint 写进去、按 32 位读出来的字段。
func readVarintU32(b []byte, off int) (val uint32, n int, err error) {
	var result uint64
	shift := uint(0)
	for off+n < len(b) {
		c := b[off+n]
		n++
		if shift < 32 {
			result |= uint64(c&0x7f) << shift
		}
		shift += 7
		if c&0x80 == 0 {
			return uint32(result), n, nil
		}
		if n > 10 {
			return 0, n, errors.New("varint 过长")
		}
	}
	return 0, n, errors.New("varint 读到文件尾")
}

// readVarintU64 读一个 64 位无符号 varint,最多 10 字节
func readVarintU64(b []byte, off int) (val uint64, n int, err error) {
	var result uint64
	shift := uint(0)
	for off+n < len(b) {
		c := b[off+n]
		n++
		if shift < 64 {
			result |= uint64(c&0x7f) << shift
		}
		shift += 7
		if c&0x80 == 0 {
			return result, n, nil
		}
		if n > 10 {
			return 0, n, errors.New("varint 过长")
		}
	}
	return 0, n, errors.New("varint 读到文件尾")
}

// Parse 解析一个(已解密的)MMKV 文件。
//
// 整个循环对损坏数据的态度是"读到哪算哪":任何一步解不动就 break,
// 把已经读出来的返回。取证场景里半个文件也是线索,直接报错等于把线索也丢了。
func Parse(data []byte) (*ParseResult, error) {
	if len(data) < 4 {
		return nil, errors.New("文件过小,读不出 MMKV 头部(至少 4 字节)")
	}
	dbSize := int(uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24)

	pos := 4
	// 跳过那个用途不明的 varint
	_, n, err := readVarintU32(data, pos)
	if err != nil {
		return nil, fmt.Errorf("解析头部失败: %w", err)
	}
	pos += n

	// dbSize 为 0 时(某些 MMKV 版本会这么写)尽量读到文件尾
	end := len(data)
	if dbSize > 0 && dbSize < end {
		end = dbSize
	}

	// 用 slice 而不是 map 收集,保持文件里的出现顺序 ——
	// map 遍历顺序是随机的,同一个文件解两次输出不一样,没法比对
	order := []string{}
	byKey := map[string][][]byte{}
	removed := 0

	for pos < end {
		keyLen, kn, err := readVarintU32(data, pos)
		if err != nil {
			break
		}
		pos += kn
		if keyLen == 0 {
			continue // 异常填充,跳过
		}
		if pos+int(keyLen) > len(data) {
			break
		}
		keyBytes := data[pos : pos+int(keyLen)]
		if !utf8.Valid(keyBytes) {
			break // 键不是合法 UTF-8,多半已经读偏了
		}
		key := string(keyBytes)
		pos += int(keyLen)

		valLen, vn, err := readVarintU32(data, pos)
		if err != nil {
			break
		}
		pos += vn
		if valLen == 0 {
			removed++
			continue
		}
		if pos+int(valLen) > len(data) {
			break
		}
		val := make([]byte, valLen)
		copy(val, data[pos:pos+int(valLen)])
		pos += int(valLen)

		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		// 新的插到前面:MMKV 是追加日志,文件后面的才是最新值
		byKey[key] = append([][]byte{val}, byKey[key]...)
	}

	entries := make([]Entry, 0, len(order))
	for _, k := range order {
		vals := byKey[k]
		views := make([]Value, 0, len(vals))
		for _, v := range vals {
			views = append(views, describeValue(v))
		}
		entries = append(entries, Entry{Key: k, Values: views})
	}
	return &ParseResult{
		Entries:      entries,
		DBSize:       dbSize,
		Consumed:     pos,
		RemovedCount: removed,
	}, nil
}
