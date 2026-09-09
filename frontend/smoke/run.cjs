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

require(path.join(__dirname, '.out.cjs'))
