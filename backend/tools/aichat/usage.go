package aichat

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// usage.jsonl 是 append-only 的请求级用量日志,每行一个 UsageRecord;
// 跨进程并发本工具不会出现(单 App 单实例),进程内用 mu 串行写入

var usageMu sync.Mutex

func usagePath() (string, error) {
	d, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "usage.jsonl"), nil
}

// appendUsageRecord 追加一条用量记录;input 与 output 都为 0 时直接跳过
//
//	(用户取消的请求或 provider 没返回 usage 时不记录)
func appendUsageRecord(r UsageRecord) error {
	if r.InputTokens == 0 && r.OutputTokens == 0 {
		return nil
	}
	path, err := usagePath()
	if err != nil {
		return err
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}

// readUsageRecords 读全量;文件不存在 → 返回空切片,不报错
func readUsageRecords() ([]UsageRecord, error) {
	path, err := usagePath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []UsageRecord{}, nil
		}
		return nil, err
	}
	defer f.Close()
	out := make([]UsageRecord, 0, 64)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r UsageRecord
		if err := json.Unmarshal(line, &r); err == nil {
			out = append(out, r)
		}
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}
