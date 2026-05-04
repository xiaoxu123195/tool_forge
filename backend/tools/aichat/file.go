package aichat

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ensureFileText 给"原生不支持文件"的协议(openai-compat)提供文本兜底:
//   - 如果 file 已有 Text(前端已经解析了 docx/xlsx/pptx/txt 等)→ 不动
//   - 如果只有 PDF base64 → 在后端用 ledongthuc/pdf 提取文本
//
// 其他协议(openai-responses / anthropic / gemini)能原生吃 PDF base64,
// 不在这里转换 — 让协议层自己用 multimodal 字段发过去
func ensureFileText(providerType ProviderType, files []FileBlock) []FileBlock {
	if providerType != TypeOpenAICompat {
		return files
	}
	out := make([]FileBlock, len(files))
	for i, f := range files {
		out[i] = f
		if f.Text != "" || f.Data == "" {
			continue
		}
		if isPDFMime(f.MimeType) || strings.HasSuffix(strings.ToLower(f.Name), ".pdf") {
			text, err := extractPDFText(f.Data)
			if err == nil && text != "" {
				out[i].Text = text
			} else if err != nil {
				out[i].Text = fmt.Sprintf("(PDF 解析失败:%s)", err.Error())
			}
		}
	}
	return out
}

func isPDFMime(m string) bool {
	return strings.EqualFold(m, "application/pdf") || strings.EqualFold(m, "application/x-pdf")
}

// extractPDFText base64 PDF → 文本(按页拼接,页间空行)
func extractPDFText(b64 string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("base64 解码失败: %w", err)
	}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("PDF 读取失败: %w", err)
	}
	var sb strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		sb.WriteString(text)
		if i < r.NumPage() {
			sb.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// fileTextChunks 把所有 FileBlock 拼成一段附加在 user message 前面的文本块
//
//	格式:
//	  --- file: foo.txt ---
//	  <text>
//
//	  --- file: bar.docx ---
//	  <text>
//
// 用于"协议不支持原生文件"时的兜底,或者所有 .txt/.code 文件统一处理
func fileTextChunks(files []FileBlock) string {
	if len(files) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, f := range files {
		if f.Text == "" {
			continue
		}
		name := f.Name
		if name == "" {
			name = "(unnamed)"
		}
		sb.WriteString("--- file: ")
		sb.WriteString(name)
		sb.WriteString(" ---\n")
		sb.WriteString(f.Text)
		sb.WriteString("\n\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// partitionFiles 按"协议是否支持原生 PDF" 把文件分两堆:
//
//	textFiles   走 prompt 文本拼接(docx/xlsx/pptx/txt/code 都在这里;
//	            如果 supportsPDF=false 则 PDF 也在这里——前提是 ensureFileText
//	            已经把 Text 提取出来)
//	binaryFiles 走协议各自的 multimodal 二进制字段(只有 supportsPDF=true 时
//	            的 PDF 会进这里)
func partitionFiles(files []FileBlock, supportsPDF bool) (textFiles, binaryFiles []FileBlock) {
	for _, f := range files {
		isPDF := isPDFMime(f.MimeType) || strings.HasSuffix(strings.ToLower(f.Name), ".pdf")
		if isPDF && supportsPDF && f.Data != "" {
			binaryFiles = append(binaryFiles, f)
			continue
		}
		if f.Text != "" {
			textFiles = append(textFiles, f)
		}
	}
	return
}

// userContentWithFileText 把 textFiles 拼到用户消息文本前
func userContentWithFileText(content string, textFiles []FileBlock) string {
	chunks := fileTextChunks(textFiles)
	if chunks == "" {
		return content
	}
	if content == "" {
		return chunks
	}
	return chunks + "\n\n" + content
}
