package diskclean

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 无效快捷方式:桌面、开始菜单、任务栏、「发送到」里,指向的东西已经不在了的那些。
//
// 判断宁可漏报不能错报。目标在网络位置、在可移动盘上、所在的盘这会儿没接上,
// 或者根本不是文件(控制面板项、应用商店应用、安装器的"广告"快捷方式),
// 都说不准它是不是真坏了 —— 这些一律不报,只说一共有几个判断不了

// shortcutPlace 一个要扫的位置
type shortcutPlace struct {
	name string
	dir  string
	// deep 往子目录里找。桌面只看第一层:桌面上的文件夹往往是工程目录,
	// 里面几万个文件,没必要为了找快捷方式全翻一遍
	deep bool
}

// BrokenShortcut 一个无效的快捷方式
type BrokenShortcut struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Location string `json:"location"`
	// Target 它指向哪儿(已经不在了)
	Target  string `json:"target"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason,omitempty"`
}

// ShortcutResult 无效快捷方式扫描的结果
type ShortcutResult struct {
	Shortcuts []BrokenShortcut `json:"shortcuts"`
	// Supported 这个系统上能不能扫。只有 Windows 有 .lnk
	Supported bool `json:"supported"`
	// Locations 扫了哪些地方
	Locations []string `json:"locations"`
	Scanned   int64    `json:"scanned"`
	// Unknown 判断不了的,没列出来
	Unknown   int64 `json:"unknown"`
	Cancelled bool  `json:"cancelled"`
	ElapsedMs int64 `json:"elapsedMs"`
}

type lnkState int

const (
	lnkOK lnkState = iota
	lnkBroken
	lnkUnknown
)

const (
	driveRemovable = 2
	driveRemote    = 4
	driveCDROM     = 5
	// maxShortcutFiles 一个位置最多看多少个文件,防着某个位置意外地大
	maxShortcutFiles = 20000
)

// judgeShortcut 一个快捷方式还有没有效。返回它指向的路径
func judgeShortcut(lnkPath string) (string, lnkState) {
	data, err := os.ReadFile(lnkPath)
	if err != nil {
		return "", lnkUnknown
	}
	info, err := parseLnk(data, decodeANSI)
	if err != nil || info.darwin {
		return "", lnkUnknown
	}

	// 记着绝对路径的几处:项目标识列表(系统自己最先用它)、LinkInfo、环境变量块。
	// 任何一处的目标还在,就算有效
	var cands []string
	if p := pathFromIDList(info.idList); p != "" {
		cands = append(cands, p)
	}
	if info.localPath != "" {
		cands = append(cands, info.localPath)
	}
	if info.envPath != "" {
		if p, ok := expandEnv(info.envPath, os.Getenv); ok {
			cands = append(cands, p)
		}
	}
	local := true
	for _, c := range cands {
		// 只在本机固定盘上看:网络路径去问一下可能卡上好几十秒,
		// U 盘、光盘、这会儿没接上的盘,不在也不代表坏了
		if !filepath.IsAbs(c) || !localFixed(c) {
			local = false
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, lnkOK
		}
	}
	// 相对路径只拿来证明"还在",不拿来证明"坏了":
	// 它是按快捷方式最初建的位置算的,快捷方式被挪过之后就对不上了
	if info.relPath != "" {
		p := filepath.Clean(filepath.Join(filepath.Dir(lnkPath), info.relPath))
		if localFixed(p) {
			if _, err := os.Stat(p); err == nil {
				return p, lnkOK
			}
		}
	}
	// 一个绝对路径都没有:控制面板、应用商店应用这类不是文件的
	if len(cands) == 0 {
		return "", lnkUnknown
	}
	if !local || info.network {
		return cands[0], lnkUnknown
	}
	switch info.driveType {
	case driveRemovable, driveRemote, driveCDROM:
		return cands[0], lnkUnknown
	}
	return cands[0], lnkBroken
}

// ScanShortcuts 找无效的快捷方式
func (s *Service) ScanShortcuts(jobID string) (*ShortcutResult, error) {
	ctx, end := s.begin(jobID)
	defer end()
	rep := s.report(jobID)
	defer rep.close()
	start := time.Now()

	places := shortcutPlaces()
	res := &ShortcutResult{Shortcuts: []BrokenShortcut{}, Locations: []string{}, Supported: len(places) > 0}
	rep.setPhase("检查快捷方式", int64(len(places)))
	seen := map[string]bool{}
	for _, pl := range places {
		if ctx.Err() != nil {
			break
		}
		res.Locations = append(res.Locations, pl.name+":"+pl.dir)
		for _, p := range listShortcuts(ctx, pl) {
			key := norm(p)
			if seen[key] {
				continue // 公用桌面和个人桌面可能指到同一个地方
			}
			seen[key] = true
			rep.setCurrent(p)
			rep.files.Add(1)
			res.Scanned++
			target, state := judgeShortcut(p)
			switch state {
			case lnkUnknown:
				res.Unknown++
				continue
			case lnkOK:
				continue
			}
			fi, err := os.Lstat(p)
			if err != nil {
				continue
			}
			v := s.guard.Check(p)
			res.Shortcuts = append(res.Shortcuts, BrokenShortcut{
				Path: p, Name: strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)),
				Location: pl.name, Target: target,
				Size: fi.Size(), ModTime: fi.ModTime().Unix(),
				Blocked: v.Blocked, Reason: v.Reason,
			})
		}
		rep.done.Add(1)
	}
	sort.SliceStable(res.Shortcuts, func(i, j int) bool { return res.Shortcuts[i].Path < res.Shortcuts[j].Path })
	res.Cancelled = ctx.Err() != nil
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
}

// listShortcuts 一个位置里的 .lnk 文件
func listShortcuts(ctx context.Context, pl shortcutPlace) []string {
	var out []string
	count := 0
	var walk func(dir string)
	walk = func(dir string) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if ctx.Err() != nil || count >= maxShortcutFiles {
				return
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			p := filepath.Join(dir, e.Name())
			switch kindOf(fi) {
			case kindDir:
				if pl.deep {
					walk(p)
				}
			case kindFile:
				count++
				if strings.EqualFold(filepath.Ext(e.Name()), ".lnk") {
					out = append(out, p)
				}
			}
		}
	}
	walk(pl.dir)
	return out
}

// DeleteShortcuts 删无效快捷方式。删之前重新判一次:目标可能已经回来了(盘接上了、软件重装了)
func (s *Service) DeleteShortcuts(req DeleteRequest) (*DeleteResult, error) {
	ctx, end := s.begin(req.JobID)
	defer end()
	rep := s.report(req.JobID)
	defer rep.close()
	rep.setPhase("删除", int64(len(req.Files)))

	res := &DeleteResult{Items: []DeleteItem{}, Recycled: !req.Permanent}
	for _, f := range req.Files {
		if ctx.Err() != nil {
			res.Cancelled = true
			break
		}
		rep.setCurrent(f.Path)
		item := DeleteItem{Path: f.Path}
		err := func() error {
			if !strings.EqualFold(filepath.Ext(f.Path), ".lnk") {
				return errors.New("不是快捷方式")
			}
			if _, state := judgeShortcut(f.Path); state != lnkBroken {
				return errors.New("它指向的东西现在找得到了,没删")
			}
			return s.removeUserFile(f, req.Permanent)
		}()
		if err != nil {
			item.Reason = err.Error()
			res.Failed++
		} else {
			item.OK = true
			res.Deleted++
			res.Bytes += f.Size
		}
		res.Items = append(res.Items, item)
		rep.done.Add(1)
	}
	return res, nil
}
