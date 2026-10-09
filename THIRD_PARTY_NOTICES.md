# 第三方组件说明

Tool Forge 本身以 GPL-3.0 发布（见 `LICENSE`）。下面是随程序一起分发的第三方组件，各自沿用原来的许可。

## scrcpy-server

- 用途：「真机浏览」里的投屏。每次投屏前推到安卓手机上运行，把屏幕编码成视频传回电脑，并执行电脑发来的触摸、按键
- 位置：`backend/tools/mirror/bundled/scrcpy-server-v5.0.1`，构建时打进可执行文件
- 版本：5.0.1，取自 scrcpy 官方发布页，未做任何修改
- 许可：Apache License 2.0，全文见 `backend/tools/mirror/bundled/LICENSE`
- 版权：Copyright (C) 2018 Genymobile；Copyright (C) 2018-2026 Romain Vimont
- 项目主页：https://github.com/Genymobile/scrcpy

## noVNC

- 用途：「真机浏览」里的 iOS 投屏。界面里的 VNC 客户端，连越狱 iPhone 上的 TrollVNC，显示画面、发送鼠标键盘
- 位置：npm 包 `@novnc/novnc`，构建时打进前端，用到 iOS 投屏时才加载
- 版本：1.7.0，取自 npm，未做任何修改
- 许可：核心代码为 Mozilla Public License 2.0；其中附带的 pako（zlib 解压）为 MIT。许可全文见包内的 `LICENSE.txt`、`docs/LICENSE.MPL-2.0` 和 `vendor/pako/LICENSE`
- 版权：Copyright (C) 2022 The noVNC authors
- 项目主页：https://github.com/novnc/noVNC

TrollVNC（iPhone 上的 VNC 服务端）不随程序分发：由用户自己编译、经工具箱装到手机上。
