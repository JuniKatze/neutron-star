# 架构决策记录

ADR 用于记录会影响长期维护的取舍，而不是记录每次普通修改。

## 索引

- [ADR-0001：采用 Wails 3 + Go + React](0001-wails-go-react.md)
- [ADR-0002：本地打包三类字体](0002-local-font-bundling.md)
- [ADR-0003：Linux 开发构建使用 GTK3](0003-gtk3-dev-backend.md)
- [ADR-0004：移除未使用的宋体资源](0004-remove-serif-font.md)

## 新增 ADR

文件名格式：

```text
0004-short-decision-name.md
```

至少包含：

- 状态
- 背景
- 决策
- 后果

若决策被后续方案取代，保留原文件并将状态改为 `Superseded`，同时链接到新 ADR。
