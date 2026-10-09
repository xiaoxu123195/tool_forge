# 投屏用的手机端程序

`scrcpy-server-v5.0.1` 是**入库的二进制**，构建时 embed 进 exe，每次投屏前推到手机的
`/data/local/tmp/scrcpy-server.jar`。程序一启动，它的清理进程就会把这个文件删掉。

| | |
|---|---|
| 来源 | scrcpy 官方发布页 v5.0.1 的 `scrcpy-server-v5.0.1` |
| 大小 | 733930 字节 |
| SHA-256 | `764eb6f79811d5211fe9df341120882ba9994c7a61b897d7bf3fb662e53bc536`（和官方 `SHA256SUMS.txt` 一致） |
| 许可 | Apache-2.0，全文见同目录的 `LICENSE` |
| 版权 | Copyright (C) 2018 Genymobile；Copyright (C) 2018-2026 Romain Vimont |

## 换一个版本

1. 从官方发布页下 `scrcpy-server-vX.Y.Z`，对一遍 `SHA256SUMS.txt`
2. 替换这里的文件，改 `../scrcpy.go` 里的 embed 文件名、`serverVersion`、`serverSHA256`
3. 对着新版本的 `doc/develop.md` 和服务端源码核对协议有没有变：
   视频头（编码 ID、会话包、帧头）、控制消息的类型编号和字节布局、启动参数
4. 跑 `go test ./backend/tools/mirror/`

**版本号必须一字不差**：启动时传给手机端的第一个参数就是版本号，对不上它直接退出。
