/**
 * 各工具的"调用示例"数据。
 *
 * 单文件集中维护,新工具暴露时在这里加一项即可。
 * 故意不放到后端 ToolHandler 接口里:
 *   - 示例文案有大量中文 + 排版,放 Go 字符串里不舒服
 *   - 字段说明 / 备注会经常调,前端改重启快
 */

export interface ExampleField {
  /** 字段名,渲染成 mono */
  name: string
  /** 是否必填(默认可选) */
  required?: boolean
  /** 类型简述,渲染在字段名旁,如 "string" / "string[]" / "number" */
  type?: string
  /** 字段说明 */
  description: string
}

export interface ExampleScenario {
  /** Tab 标题,如 "iOS 国区" */
  label: string
  /** 场景简述,可选 */
  hint?: string
  /** 真实可跑的 body 对象 */
  body: Record<string, unknown>
}

export interface ToolExampleSet {
  scenarios: ExampleScenario[]
  fields?: ExampleField[]
  /** 附加说明,渲染成 bullet 列表 */
  notes?: string[]
  /** 流式(SSE)工具;影响 curl 命令的 -N 标志和提示文案 */
  streaming?: boolean
  /** 敏感工具,工具列表行加 ⚠ 标记 */
  sensitive?: boolean
}

export const TOOL_EXAMPLES: Record<string, ToolExampleSet> = {
  'app-search': {
    scenarios: [
      {
        label: 'iOS 国区',
        hint: '搜国区 App Store',
        body: { keyword: '微信', sources: ['itunes'], country: 'cn' },
      },
      {
        label: 'iOS 美区',
        hint: '换 country 即换区域',
        body: { keyword: 'wechat', sources: ['itunes'], country: 'us' },
      },
      {
        label: 'iOS 七麦',
        hint: '需要先在"外部工具"配好七麦 PHPSESSID',
        body: { keyword: '微信', sources: ['qimai_ios'], country: 'cn' },
      },
      {
        label: 'Android 应用宝',
        hint: '应用宝不需要 PHPSESSID',
        body: { keyword: '微信', sources: ['yingyongbao'] },
      },
      {
        label: 'Android 七麦 · 华为',
        hint: '需要 PHPSESSID; market=6 表示华为应用市场',
        body: { keyword: '微信', sources: ['qimai_android'], market: 6 },
      },
      {
        label: 'Google Play',
        hint: '走 Google Play 商店搜索',
        body: { keyword: 'wechat', sources: ['googleplay'], country: 'us' },
      },
      {
        label: '同时搜多源',
        hint: 'sources 数组多选,所有源并发执行,任一失败不影响其他',
        body: { keyword: '微信', sources: ['itunes', 'yingyongbao'], country: 'cn' },
      },
      {
        label: '需要更多结果',
        hint: '默认每源 5 条,传 limit_per_source 可拿更多(最大 50)',
        body: { keyword: '微信', sources: ['itunes'], country: 'cn', limit_per_source: 20 },
      },
    ],
    fields: [
      { name: 'keyword', required: true, type: 'string', description: '搜索关键词,可填应用名或包名' },
      {
        name: 'sources',
        type: 'string[]',
        description: '搜索源数组,可多选;不传时按 iOS 默认走 [itunes]',
      },
      {
        name: 'country',
        type: 'string',
        description: '国家码(cn / us / jp / gb / ...),iOS 系列源用,默认 cn',
      },
      {
        name: 'market',
        type: 'number',
        description: 'Android 厂商市场 ID,仅 qimai_android 用;见下方备注',
      },
      {
        name: 'limit_per_source',
        type: 'number',
        description: '每个源最多返回多少条,默认 5,上限 50;90% 场景目标 App 在前 1-2 条,不用调大',
      },
    ],
    notes: [
      'sources 取值: itunes · qimai_ios · qimai_android · yingyongbao · googleplay',
      'Android market ID: 华为=6 · 应用宝=3 · 小米=4 · OPPO=9 · VIVO=8 · 魅族=7 · 百度=2 · 360=1 · 豌豆荚=5 · GooglePlay=10 · 鸿蒙=11',
      '七麦的两个源(qimai_ios / qimai_android)需要在 Profile → 外部工具里配好 PHPSESSID,否则会返回登录失败',
      '所有源并发执行,响应里 statuses 字段会列出每个源的执行结果(成功 / 失败原因 / 命中数)',
    ],
  },
  'mobile-forensic': {
    streaming: true,
    sensitive: true,
    scenarios: [
      {
        label: 'Android 关键字搜索',
        hint: '从 root 设备抽取所有匹配关键字的 App 数据',
        body: { args: ['android', 'export', '-k', 'wechat', '-o', '/tmp/forensic'] },
      },
      {
        label: 'Android 指定包名',
        hint: '-k 也可以传完整包名,更精准',
        body: { args: ['android', 'export', '-k', 'com.tencent.mm', '-o', '/tmp/forensic'] },
      },
      {
        label: 'iOS via USB',
        hint: '默认走 USB 代理(usbmuxd),需要设备已信任电脑',
        body: { args: ['ios', 'export', '-k', 'wechat', '-o', '/tmp/forensic'] },
      },
      {
        label: 'iOS via SSH',
        hint: '越狱设备走 SSH;-p 是 root 密码,默认 alpine',
        body: {
          args: [
            'ios',
            'export',
            '-k',
            'wechat',
            '-o',
            '/tmp/forensic',
            '-a',
            'root@127.0.0.1:22',
            '-p',
            'alpine',
            '-u=false',
          ],
        },
      },
      {
        label: '只抽指定路径',
        hint: '-s 指定 App 内部具体路径,跳过全量扫描',
        body: {
          args: [
            'android',
            'export',
            '-k',
            'com.tencent.mm',
            '-s',
            '/data/data/com.tencent.mm/databases',
            '-o',
            '/tmp/forensic',
          ],
        },
      },
    ],
    fields: [
      {
        name: 'args',
        required: true,
        type: 'string[]',
        description: '完整 go-forensic CLI 参数;首参数是平台(android/ios),次参数是子命令(export)',
      },
    ],
    notes: [
      '✱ 响应是 SSE 流(text/event-stream),用 curl -N 或 EventSource 接收',
      '事件格式: data: {"type":"log","data":{...}}\\n\\n; type 取值: started / log / done / error',
      'log 事件的 data 包含 stream(stdout/stderr) 和 line;done 事件的 data 含 exitCode 和 canceled',
      '客户端关闭连接(curl Ctrl+C / EventSource.close) → 后端自动 Cancel,杀掉 go-forensic 进程',
      '需要先在 Profile → 外部工具中配置 go-forensic 可执行文件路径,否则会立刻返回 error',
      '⚠ 此工具会调用外部 CLI 访问 Android / iOS 设备数据,涉及隐私敏感操作,强烈建议在 "本地 API" 里开启 Token 鉴权',
    ],
  },
}
