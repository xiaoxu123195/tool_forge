// 冒烟测试里替掉 noVNC:它要真的 VNC 服务端,jsdom 里也没有画布。
// 假的这个照真的样子往 target 里放一块画布,把按键、剪贴板记下来;
// probe 经 instances 拿到它,模拟「连上」「手机复制了东西」「断开」
const instances = []

class FakeRFB extends EventTarget {
  constructor(target, channel, options) {
    super()
    this.target = target
    this.channel = channel
    this.options = options || {}
    this.keys = []
    this.clipboard = []
    this.qualityLevel = 6
    this.scaleViewport = false
    this.focusOnClick = true
    this.background = ''
    this.disconnected = false
    // 对方报过来的剪贴板能力,和真的 noVNC 同名:连上以后才有
    this._clipboardServerCapabilitiesFormats = {}
    this._clipboardServerCapabilitiesActions = {}
    const screen = document.createElement('div')
    this.canvas = document.createElement('canvas')
    this.canvas.width = 0
    this.canvas.height = 0
    screen.appendChild(this.canvas)
    target.appendChild(screen)
    instances.push(this)
  }

  sendKey(keysym, _code, down) {
    this.keys.push([keysym, down])
  }

  clipboardPasteFrom(text) {
    this.clipboard.push(text)
  }

  sendCredentials() {}

  disconnect() {
    this.disconnected = true
  }

  toBlob(cb, type) {
    cb(new window.Blob([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], { type }))
  }

  // ---- 给 probe 用的 ----

  /**
   * 握手完成:画布有了手机的尺寸。剪贴板能力照 TrollVNC(libvncserver)真报的来:
   * 收文字,能问、能看、能「直接给」,唯独没有「通知」
   */
  connect(w, h) {
    this.canvas.width = w
    this.canvas.height = h
    this._clipboardServerCapabilitiesFormats = { 1: true }
    this._clipboardServerCapabilitiesActions = {}
    for (let i = 24; i <= 31; i++) this._clipboardServerCapabilitiesActions[1 << i] = !!(0x17000000 & (1 << i))
    this.dispatchEvent(new Event('connect'))
  }

  /** 手机上复制了东西 */
  pushClipboard(text) {
    this.dispatchEvent(new CustomEvent('clipboard', { detail: { text } }))
  }

  /** 连接断了(拔线、服务停了) */
  drop() {
    this.dispatchEvent(new CustomEvent('disconnect', { detail: { clean: false } }))
  }
}

// 打成 Node 单文件后,组件里 import() 拿到的 default 是整个 module.exports(Node 的规矩),
// 所以让 module.exports 本身就是这个类,default、instances 挂在它上面
module.exports = FakeRFB
module.exports.default = FakeRFB
module.exports.instances = instances
