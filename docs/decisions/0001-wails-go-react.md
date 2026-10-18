# ADR-0001：采用 Wails 3 + Go + React

- 状态：Accepted
- 日期：2026-10-17

## 背景

项目需要一个可本地运行的桌面 Agent，既能使用 Go 访问系统与模型适配器，又能快速构建复杂的聊天和 Markdown 界面。

## 决策

- 桌面容器使用 Wails 3。
- 系统能力、模型连接和桥接层使用 Go。
- 前端使用 React 18、TypeScript 和 Vite。
- Go 服务通过 Wails 生成 TypeScript 绑定和事件类型。

## 后果

- 前后端通过 Wails 方法和事件通信；具体契约目前仍是可迁移的临时桥接层。
- 前端可以使用成熟的 React/CodeMirror/Markdown 生态。
- 需要维护 Go、Node 和 Wails 三套开发依赖。
- `frontend/bindings/**` 是生成产物，不能手工编辑。
- 原生窗口行为可能受 Wails 版本和平台实现影响，需要真实窗口验证。
