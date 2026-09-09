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

// jsdom 没实现 scrollIntoView(它不做布局),但真实浏览器里每个元素都有。
// 不补的话,任何"跳到某条消息"的代码在这里都会炸,而那是 jsdom 的缺口、不是应用的 bug。
// 补成空函数即可 —— 冒烟测试要验的是"渲染不炸",不是"滚没滚到位"。
if (!dom.window.Element.prototype.scrollIntoView) {
  dom.window.Element.prototype.scrollIntoView = function () {}
}

require(path.join(__dirname, '.out.cjs'))
