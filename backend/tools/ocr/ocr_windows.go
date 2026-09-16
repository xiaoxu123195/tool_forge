//go:build windows

package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

// 走 PowerShell 调 WinRT 的 Windows.Media.Ocr。
//
// Go 直接调 WinRT 要 COM 那一整套,还得 cgo;PowerShell 5.1 自带 WinRT 投影,
// 一段脚本就能把引擎拉起来。代价是每次识别多花几百毫秒起进程 —— 手动点一下的功能,
// 这点等待可以接受。
//
// 脚本用 -EncodedCommand 传:图片路径里有空格、引号、中文,拼进命令行迟早出事;
// 整段脚本 base64 一下,路径只在脚本里出现一次,用单引号并把单引号翻倍。
const script = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
try {
  $null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType=WindowsRuntime]
  $null = [Windows.Storage.StorageFile, Windows.Storage, ContentType=WindowsRuntime]
  $null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics.Imaging, ContentType=WindowsRuntime]
  $null = [Windows.Globalization.Language, Windows.Globalization, ContentType=WindowsRuntime]
  $null = [Windows.Foundation.IAsyncOperation` + "`" + `1, Windows.Foundation, ContentType=WindowsRuntime]
  Add-Type -AssemblyName System.Runtime.WindowsRuntime
  $asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
    $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1'
  })[0]
  function Await($op, $type) {
    $task = $asTask.MakeGenericMethod($type).Invoke($null, @($op))
    $null = $task.Wait(-1)
    return $task.Result
  }
  $file = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync('__PATH__')) ([Windows.Storage.StorageFile])
  $stream = Await ($file.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
  $decoder = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
  $bitmap = Await ($decoder.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
  $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
  if ($null -eq $engine) {
    foreach ($tag in @('zh-Hans-CN', 'zh-CN', 'en-US')) {
      $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage((New-Object Windows.Globalization.Language $tag))
      if ($null -ne $engine) { break }
    }
  }
  if ($null -eq $engine) {
    Write-Output 'ERR:NOLANG'
    exit 0
  }
  $result = Await ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
  $lines = @()
  foreach ($l in $result.Lines) { $lines += ,$l.Text }
  $obj = @{ lang = $engine.RecognizerLanguage.LanguageTag; lines = $lines }
  Write-Output (ConvertTo-Json -InputObject $obj -Compress -Depth 3)
} catch {
  Write-Output ('ERR:' + $_.Exception.Message)
}
`

const ocrTimeout = 40 * time.Second

func recognize(ctx context.Context, imagePath string) (*Result, error) {
	abs, err := filepath.Abs(imagePath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("图片不存在: %w", err)
	}
	ps := strings.Replace(script, "__PATH__", strings.ReplaceAll(abs, "'", "''"), 1)

	ctx, cancel := context.WithTimeout(ctx, ocrTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeCommand(ps))
	// Wails 是 GUI 子系统,不藏的话会闪一个黑框
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("系统 OCR 超过 %s 没有返回", ocrTimeout)
		}
		return nil, fmt.Errorf("调用系统 OCR 失败: %v %s", err, firstLine(stderr.String()))
	}
	out := strings.TrimSpace(stdout.String())
	if strings.HasPrefix(out, "ERR:") {
		msg := strings.TrimSpace(strings.TrimPrefix(out, "ERR:"))
		if msg == "NOLANG" {
			return nil, errors.New("系统里没有可用的 OCR 语言包：设置 → 时间和语言 → 语言，给中文或英文装上「光学字符识别」")
		}
		return nil, fmt.Errorf("系统 OCR 报错: %s", msg)
	}
	var payload struct {
		Lang  string   `json:"lang"`
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return nil, fmt.Errorf("系统 OCR 的输出读不懂: %v: %s", err, firstLine(out))
	}
	return assemble(payload.Lang, payload.Lines), nil
}

// encodeCommand PowerShell 的 -EncodedCommand 要 UTF-16LE 的 base64
func encodeCommand(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
