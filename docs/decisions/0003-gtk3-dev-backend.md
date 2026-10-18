# ADR-0003：Linux 开发构建使用 GTK3

- 状态：Accepted（临时）
- 日期：2026-10-17

## 背景

当前使用 Wails `v3.0.0-beta.29`。Linux GTK4 后端在无边框窗口开始拖动时，会把 WebView 控件的局部坐标当作屏幕坐标传给窗口管理器，导致鼠标跳到固定位置。

相关上游问题：<https://github.com/wailsapp/wails/issues/6257>

## 决策

开发构建附加 `gtk3` tag：

```text
wails3 build DEV=true EXTRA_TAGS=gtk3
```

配置位置：`build/config.yml`。

## 后果

- 顶栏拖动使用 GTK3 的正确屏幕坐标路径。
- Linux 开发环境需要 GTK3 和 WebKitGTK 4.1 开发库。
- 这是针对 Wails beta 版本的临时绕行，不代表长期锁定 GTK3。
- 上游修复后需要重新验证 GTK4，并在确认拖动、缩放和窗口按钮正常后移除该 tag。
