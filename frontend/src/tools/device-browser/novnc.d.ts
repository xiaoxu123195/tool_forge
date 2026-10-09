// noVNC 的包没带类型。这里只声明 iOS 投屏用到的那几样,和它的 docs/API.md 对得上
declare module '@novnc/novnc' {
  export interface RfbOptions {
    shared?: boolean
    credentials?: { username?: string; password?: string; target?: string }
    wsProtocols?: string[]
  }

  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, urlOrChannel: string | WebSocket, options?: RfbOptions)
    viewOnly: boolean
    focusOnClick: boolean
    clipViewport: boolean
    scaleViewport: boolean
    resizeSession: boolean
    showDotCursor: boolean
    background: string
    qualityLevel: number
    compressionLevel: number
    disconnect(): void
    sendCredentials(credentials: { username?: string; password?: string }): void
    sendKey(keysym: number, code: string | null, down?: boolean): void
    clipboardPasteFrom(text: string): void
    toBlob(callback: (blob: Blob | null) => void, type?: string, quality?: number): void
  }
}
