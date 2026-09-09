// Wails RPC 薄封装。直接走 window.go.main.App,避免依赖 wailsjs 生成的具体类型。
// 函数签名与 backend/app.go 里的 RPC 一一对应。

import type {
  AccountPatch,
  AccountSecret,
  AccountView,
  AuthURLResult,
  Config,
  ExchangeResult,
  ExportResult,
  ExportSummary,
  ExtractResult,
  Folder,
  Group,
  ImportRequest,
  ImportResponse,
  MailDetail,
  MailPage,
  RefreshJobState,
  RefreshResult,
  SaveFromAuthRequest,
} from './types'

interface AppRPC {
  ListOutlookGroups(): Promise<Group[] | null>
  AddOutlookGroup(name: string, color: string): Promise<Group | null>
  RenameOutlookGroup(id: string, name: string): Promise<string>
  DeleteOutlookGroup(id: string): Promise<string>

  ListOutlookAccounts(groupID: string): Promise<AccountView[] | null>
  ImportOutlookAccounts(req: ImportRequest): Promise<ImportResponse>
  UpdateOutlookAccount(id: string, patch: AccountPatch): Promise<AccountView | null>
  DeleteOutlookAccount(id: string): Promise<string>

  RefreshOutlookToken(id: string): Promise<RefreshResult>
  RefreshOutlookTokens(ids: string[]): Promise<RefreshResult[] | null>

  ListOutlookMails(accountID: string, folder: string, page: number, pageSize: number): Promise<MailPage | null>
  GetOutlookMail(accountID: string, folder: string, messageID: string): Promise<MailDetail | null>
  ExtractOutlookMail(accountID: string, folder: string, messageID: string): Promise<ExtractResult | null>
  ExtractOutlookText(text: string): Promise<ExtractResult | null>

  GetOutlookConfig(): Promise<Config>
  UpdateOutlookConfig(cfg: Config): Promise<string>

  GetOutlookAccountSecret(id: string): Promise<AccountSecret | null>
  SetOutlookRefreshToken(id: string, newRT: string): Promise<string>

  BuildOutlookAuthURL(clientID: string, redirectURI: string): Promise<AuthURLResult>
  ExchangeOutlookCode(redirectedURL: string, clientID: string, redirectURI: string): Promise<ExchangeResult | null>
  SaveOutlookFromAuth(req: SaveFromAuthRequest): Promise<AccountView | null>

  PreviewOutlookExport(): Promise<ExportSummary[] | null>
  ExportOutlookAccounts(groupIDs: string[]): Promise<ExportResult | null>
  PickOutlookExportPath(defaultFilename: string): Promise<string>
  WriteOutlookExportFile(path: string, content: string): Promise<string>

  StartOutlookRefreshJob(ids: string[]): Promise<string>
  CancelOutlookRefreshJob(jobID: string): Promise<void>
  GetOutlookRefreshJob(jobID: string): Promise<RefreshJobState | null>
  ListOutlookActiveRefreshJobs(): Promise<RefreshJobState[] | null>
  ListOutlookRefreshHistory(): Promise<RefreshJobState[] | null>
}

function app(): AppRPC {
  const g = window as unknown as { go?: { main?: { App?: AppRPC } } }
  const a = g.go?.main?.App
  if (!a) throw new Error('Wails bridge 未就绪')
  return a
}

// 把 Go 双返回 (T, string) 规范成抛出 string 错误。
//
// 注意 Wails 对多返回值的序列化在不同 Wails 版本/不同场景下表现不一致,可能是:
//   1) 数组 [T, string]
//   2) 对象 { '0': T, '1': string }
//   3) 仅 T 本身(当 string 是空字符串时 Wails 可能省掉包装)
// 必须三种都兼容,所以不能用解构,也不能假定 r[0] 一定是 T。
/**
 * 取绑定的返回值。
 *
 * 后端这些方法现在都是 (T, error):Wails 只把第一个值交给 JS,出错时直接 reject。
 * 以前那套 [值, 错误] 的解包分支全是死代码 —— 错误字符串从来没到过这里。
 */
async function unwrap<T>(p: Promise<T | null>): Promise<T> {
  const v = await p
  if (v == null) throw new Error('返回值为空')
  return v
}
async function unwrapErr(p: Promise<string>): Promise<void> {
  const err = await p
  if (err) throw new Error(err)
}

export const outlookAPI = {
  listGroups: () => app().ListOutlookGroups().then((g) => g ?? []),
  addGroup: (name: string, color = '') => unwrap(app().AddOutlookGroup(name, color)),
  renameGroup: (id: string, name: string) => unwrapErr(app().RenameOutlookGroup(id, name)),
  deleteGroup: (id: string) => unwrapErr(app().DeleteOutlookGroup(id)),

  listAccounts: (groupID = '') => app().ListOutlookAccounts(groupID).then((a) => a ?? []),
  importAccounts: (req: ImportRequest) => app().ImportOutlookAccounts(req),
  updateAccount: (id: string, patch: AccountPatch) => unwrap(app().UpdateOutlookAccount(id, patch)),
  deleteAccount: (id: string) => unwrapErr(app().DeleteOutlookAccount(id)),

  refreshOne: (id: string) => app().RefreshOutlookToken(id),
  refreshMany: (ids: string[]) => app().RefreshOutlookTokens(ids).then((r) => r ?? []),

  listMails: (accountID: string, folder: Folder, page: number, pageSize = 20) =>
    unwrap(app().ListOutlookMails(accountID, folder, page, pageSize)),
  getMail: (accountID: string, folder: Folder, messageID: string) =>
    unwrap(app().GetOutlookMail(accountID, folder, messageID)),
  extractMail: (accountID: string, folder: Folder, messageID: string) =>
    unwrap(app().ExtractOutlookMail(accountID, folder, messageID)),
  extractText: (text: string) => app().ExtractOutlookText(text),

  getConfig: () => app().GetOutlookConfig(),
  updateConfig: (cfg: Config) => unwrapErr(app().UpdateOutlookConfig(cfg)),

  getAccountSecret: (id: string) => unwrap(app().GetOutlookAccountSecret(id)),
  setRefreshToken: (id: string, rt: string) => unwrapErr(app().SetOutlookRefreshToken(id, rt)),

  buildAuthURL: (clientID = '', redirectURI = '') =>
    app().BuildOutlookAuthURL(clientID, redirectURI),
  exchangeCode: (redirectedURL: string, clientID = '', redirectURI = '') =>
    unwrap(app().ExchangeOutlookCode(redirectedURL, clientID, redirectURI)),
  saveFromAuth: (req: SaveFromAuthRequest) => unwrap(app().SaveOutlookFromAuth(req)),

  previewExport: () => app().PreviewOutlookExport().then((s) => s ?? []),
  exportAccounts: (groupIDs: string[]) => unwrap(app().ExportOutlookAccounts(groupIDs)),
  pickExportPath: (defaultFilename = '') => app().PickOutlookExportPath(defaultFilename),
  writeExportFile: (path: string, content: string) =>
    unwrapErr(app().WriteOutlookExportFile(path, content)),

  startRefreshJob: (ids: string[]) => app().StartOutlookRefreshJob(ids),
  cancelRefreshJob: (jobID: string) => app().CancelOutlookRefreshJob(jobID),
  getRefreshJob: (jobID: string) => app().GetOutlookRefreshJob(jobID),
  listActiveRefreshJobs: () => app().ListOutlookActiveRefreshJobs().then((r) => r ?? []),
  listRefreshHistory: () => app().ListOutlookRefreshHistory().then((r) => r ?? []),
}
