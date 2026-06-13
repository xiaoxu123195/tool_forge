package aichat

import (
	"bufio"
	"io"
	"strings"
)

// sseScanner 逐行读取 SSE(text/event-stream)响应。
//
// 为什么不用 bufio.Scanner:生图场景下,中转会把一整张 base64 图片塞进**一行**
// data: 里(动辄数 MB),远超 bufio.Scanner 的单 token 上限,导致
// "bufio.Scanner: token too long"。bufio.Reader.ReadString 对单行长度没有上限,
// 缓冲按需增长,可以安全读取超长行。
//
// 暴露与 bufio.Scanner 相同的 Scan / Text / Err 三件套,便于原地替换。
type sseScanner struct {
	r    *bufio.Reader
	line string
	err  error
}

func newSSEScanner(r io.Reader) *sseScanner {
	return &sseScanner{r: bufio.NewReaderSize(r, 64*1024)}
}

// Scan 读取下一行;返回 false 表示流结束或出错(用 Err 区分)。
// 与 bufio.Scanner 一致:返回的行已去掉行尾的 \r\n。
func (s *sseScanner) Scan() bool {
	if s.err != nil {
		return false
	}
	line, err := s.r.ReadString('\n')
	if err != nil {
		s.err = err
		if line == "" {
			return false
		}
		// 末行没有换行符但有内容:本行仍然有效,下一次 Scan 再结束
	}
	s.line = strings.TrimRight(line, "\r\n")
	return true
}

func (s *sseScanner) Text() string { return s.line }

// Err 返回读取过程中的非 EOF 错误(EOF 视为正常结束)。
func (s *sseScanner) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}
