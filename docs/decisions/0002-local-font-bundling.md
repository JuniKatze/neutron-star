# ADR-0002：本地打包三类字体

- 状态：Superseded by [ADR-0004](0004-remove-serif-font.md)
- 日期：2026-10-17

## 背景

仅依赖系统字体时，不同 Linux 发行版和桌面环境会得到不同的中文、西文和代码字体。项目需要稳定的视觉层级和离线可用性。

## 决策

本地打包三类开源字体，并通过语义令牌统一使用：

- `--font-sans`：Noto Sans SC 子集，本地名 `Neutron Sans`
- `--font-serif`：Noto Serif SC 子集，本地名 `Neutron Serif`
- `--font-mono`：Maple Mono CN，本地名 `Neutron Mono`

字体文件使用 WOFF2，放在 `frontend/src/assets/fonts/`。

## 后果

- 字体渲染不依赖用户系统中的同名字体。
- 应用包增加约 23 MB。
- 字体族切换只需修改 `frontend/src/styles/fonts.css`。
- 字体文件的许可证必须随发行物保留。
- 新增字重、替换字体或扩大字符集时，需要重新检查体积和加载效果。
