# 内置 adb 的载荷

`platform-tools.tar.gz` 是**入库的二进制**，构建时会被 embed 进 exe，
用户第一次连设备时解到 `~/.toolforge/platform-tools/`。

当前这份：

| | |
|---|---|
| 来源 | Android SDK Platform-Tools |
| adb 版本 | 1.0.41 / 37.0.1-15733141 |
| 压缩后 | 3.86 MB |
| 对 exe 的影响 | +3.87 MB（29.50 → 33.37 MB） |

## 里面只有三个文件

```
adb.exe
AdbWinApi.dll
AdbWinUsbApi.dll
```

平铺，**不带目录层级** —— adb.exe 是在自己旁边找那两个 DLL 的。

实测这三个就能跑起来（`libwinpthread-1.dll` 也不需要）。整包 17 MB 里的
fastboot、sqlite3、mke2fs、etc1tool、NOTICE.txt 一个都用不上，
带上只是让每个用户多下十几 MB。

## 怎么换一份

```sh
scripts/pack-adb.sh <platform-tools 目录>
```

脚本只挑上面那三个文件、按同样的方式压。换完跑一遍
`go test ./backend/tools/adbx/` —— 那里的用例会把它解出来**真的执行一次**，
版本不够新（末位 < 40，认不出 2013 年以后的设备）会直接红。

载荷换了不用改任何代码：解包标记记的是载荷的哈希，对不上自动重解。

## 没有这个文件也能编

`bundled/` 里没有 `.tar.gz` 时整套内置 adb 自动关掉：代码照常编译、照常跑，
只是回落到"用系统 PATH 上的 adb"。所以缺了它不会把构建弄断 ——
但发出去的版本就不带 adb 了，发版前记得确认它在。
