// jsdom 环境 + 跑打包产物。React 的 act 需要 IS_REACT_ACT_ENVIRONMENT。
const { JSDOM } = require('jsdom')
const path = require('path')

const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/',
  pretendToBeVisual: true,
})
global.window = dom.window
global.document = dom.window.document
global.navigator = dom.window.navigator
global.HTMLElement = dom.window.HTMLElement
global.Element = dom.window.Element
global.Node = dom.window.Node
global.getComputedStyle = dom.window.getComputedStyle
global.requestAnimationFrame = (cb) => setTimeout(() => cb(Date.now()), 0)
global.cancelAnimationFrame = clearTimeout
global.IS_REACT_ACT_ENVIRONMENT = true

// CodeMirror(plist / JSON 编辑器)在挂载时就要这几样。jsdom 有 MutationObserver,
// 只是挂在 window 上没进全局;ResizeObserver 它干脆没有。
// 这些都是 jsdom 的缺口,补上才能让带编辑器的页面进冒烟测试
global.Window = dom.window.Window
global.MutationObserver = dom.window.MutationObserver
global.DOMRect = dom.window.DOMRect
global.ResizeObserver =
  dom.window.ResizeObserver ||
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
dom.window.ResizeObserver = global.ResizeObserver

// jsdom 没实现 scrollIntoView(它不做布局),但真实浏览器里每个元素都有。
// 不补的话,任何"跳到某条消息"的代码在这里都会炸,而那是 jsdom 的缺口、不是应用的 bug。
// 补成空函数即可 —— 冒烟测试要验的是"渲染不炸",不是"滚没滚到位"。
if (!dom.window.Element.prototype.scrollIntoView) {
  dom.window.Element.prototype.scrollIntoView = function () {}
}

// jsdom 也没有 matchMedia。代码编辑器靠它判断当前是不是暗色主题,
// 缺了就是整个组件挂载即抛 —— 同样是 jsdom 的缺口,不是应用的 bug。
// 一律回答"不匹配"(= 亮色),冒烟测试验的是渲染不炸,不是配色对不对。
if (!dom.window.matchMedia) {
  dom.window.matchMedia = (query) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  })
}

require(path.join(__dirname, '.out.cjs'))
