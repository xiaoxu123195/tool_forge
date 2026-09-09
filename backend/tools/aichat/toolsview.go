package aichat

import (
	"sort"
	"strings"
)

// ChatToolInfo 一个会声明给模型的工具,给界面看的形态
type ChatToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Source 工具来自哪儿:内置工具为空,MCP 工具是服务器名
	Source string `json:"source,omitempty"`
}

// ChatToolsView 「工具」开关背后到底带了什么。
//
// 输入栏那个开关原来是个纯粹的黑盒:打开之后模型手里有哪些工具、哪台 MCP 服务器
// 没连上因而这一轮用不了 —— 界面上一个字都没有。而没连上的服务器是**静默跳过**的
// (聊天路径用 CachedTools,绝不现连),用户根本不知道自己少了东西。
type ChatToolsView struct {
	Tools []ChatToolInfo `json:"tools"`
	// Offline 已启用但当前没连上的 MCP 服务器名 —— 它们的工具这一轮不会被声明
	Offline []string `json:"offline,omitempty"`
}

// ToolsView 返回当前这一刻会声明给模型的工具清单。
//
// 和 listTools 走同一份数据(CachedTools),所以界面上看到的就是模型真正拿到的,
// 不会出现"界面说有、实际没带"的偏差。
func ToolsView() ChatToolsView {
	view := ChatToolsView{Tools: []ChatToolInfo{}}
	for _, t := range builtinTools {
		view.Tools = append(view.Tools, ChatToolInfo{
			Name:        t.Name,
			Description: t.Description,
		})
	}

	svc := currentMCP()
	if svc == nil {
		sortTools(view.Tools)
		return view
	}

	connected := map[string]bool{}
	for _, info := range svc.CachedTools() {
		connected[info.ServerName] = true
		view.Tools = append(view.Tools, ChatToolInfo{
			Name:        info.QualifiedName,
			Description: info.Description,
			Source:      info.ServerName,
		})
	}
	sortTools(view.Tools)

	// 启用了却没在 CachedTools 里出现的,就是还没连上(或连挂了)的
	for _, srv := range svc.EnabledServerNames() {
		if !connected[srv] {
			view.Offline = append(view.Offline, srv)
		}
	}
	sort.Strings(view.Offline)
	return view
}

// sortTools 内置的排前面,其余按名字 —— 内置工具数量少且稳定,固定在顶部便于辨认
func sortTools(list []ChatToolInfo) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if (a.Source == "") != (b.Source == "") {
			return a.Source == ""
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}
