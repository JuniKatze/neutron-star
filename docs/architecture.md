# 系统架构

> **文档状态**：本文首先是当前实现的快照，不是最终 `internal/**` 领域设计。`main.go` 和 `service.go` 是 UI 开发阶段的临时脚手架，其中的方法、DTO 和职责边界都可能迁移或重构。

## 文档边界

- “目标”描述仍然成立的产品和技术方向。
- “当前实现”描述仓库现在能运行的行为。
- 根目录 `service.go` 中的 `ChatStatus`、`ChatDelta`、`ChatOutcome`、`ChatTurn` 当前承担 Wails 传输和界面渲染职责，其包位置不是最终架构决定。
- 会话状态、工具执行、权限、持久化等能力后续可能由 `internal/**` 承担；不得根据当前脚手架推断这些能力永远留在前端或根目录。
- 目标领域模型尚未在文档中定稿。`internal` 设计稳定后，再增加目标架构图并更新契约。

## 目标

`neutron-star` 是一个本地桌面 AI 工作台。当前架构优先保证三件事：

1. 对话内容可以持续流式呈现，而不是等完整回答结束后一次性显示。
2. 前端通过可替换的 Wails 桥接层访问后端，不直接依赖模型适配器实现。
3. `internal/llm` 作为生产代码区域，与界面、窗口和交互逻辑隔离。

## 总体结构

```text
┌────────────────────────────────────────────────────────────┐
│ Wails WebView                                              │
│                                                            │
│  React UI                                                  │
│    App / Conversation / MessageView / Composer             │
│            │                                               │
│            ▼                                               │
│  useChatService                                            │
│    Status / Send / Stop / Reload                           │
│            │                                               │
└────────────┼───────────────────────────────────────────────┘
             │ Wails bindings + events
             ▼
┌────────────────────────────────────────────────────────────┐
│ Go application                                             │
│                                                            │
│  ChatService (temporary service.go)                        │
│    config loading · request bridging · cancellation        │
│            │                                               │
│            ▼                                               │
│  internal/llm                                              │
│    request model · streaming adapter · response parsing    │
│            │                                               │
│            ▼                                               │
│  Chat Completions endpoint                                 │
└────────────────────────────────────────────────────────────┘
```

## 当前启动流程

1. `main.go` 创建 Wails 应用，注册 `ChatService`。
2. 应用创建 900×640 的无边框窗口，最小尺寸为 560×420。
3. `ChatService.ServiceStartup` 读取用户配置并创建模型适配器。
4. 前端挂载后调用 `ChatService.Status()`。
5. `Status().configured` 决定输入框是否可用，以及是否显示连接配置提示。

## 前端结构

| 路径 | 职责 |
| --- | --- |
| `frontend/src/App.tsx` | 应用框架、顶栏、侧栏、快捷键和主要状态组合 |
| `frontend/src/chat/useChatService.ts` | Wails 调用、流式事件订阅、消息状态和增量合并 |
| `frontend/src/components/Conversation.tsx` | 对话滚动与自动跟随 |
| `frontend/src/components/MessageView.tsx` | 文本、推理、工具提示和错误提示 |
| `frontend/src/components/MarkdownRenderer.tsx` | Markdown、GFM、代码块和复制 |
| `frontend/src/components/Composer.tsx` | CodeMirror 输入、发送、停止和 IME 处理 |
| `frontend/src/components/WindowControls.tsx` | 无边框窗口的最小化、最大化和关闭 |
| `frontend/src/styles/fonts.css` | 本地字体与字体族令牌 |
| `frontend/src/styles/style.css` | 全局布局、组件样式和响应式规则 |

`frontend/bindings/**` 由 Wails 生成，是 Go 服务到 TypeScript 的机械映射，不是手工维护的源文件。

## 一次对话的生命周期

```text
Composer.submit
  → useChatService.send
    → 前端加入 user 与 pending assistant 消息
    → ChatService.Send(prompt, history)
      → Go 创建 request 并启动 stream goroutine
    → Send 返回

stream goroutine
  → chat:delta  （多次）
  → chat:done   （正常完成）
     或
  → chat:error  （失败或取消）

useChatService
  → 合并增量到对应 assistant 消息
  → 最终用完整正文校准界面
  → 结束 running 状态
```

流式增量在前端通过 `requestAnimationFrame` 合并提交，减少逐 token 重渲染和 Markdown 重解析。

## 当前状态所有权

- 对话消息由前端保存在内存中。
- 每次发送时，前端把已有正文历史传给 `ChatService.Send`。
- Go 服务不保存会话历史。
- 模型连接配置由 Go 端读取，前端通过 `ChatStatus` 获得展示所需字段。

在当前脚手架中，应用重启会丢失界面中的对话，但不会造成前后端历史版本不一致。若后续把会话持久化或领域状态迁入 `internal`，需要重新定义所有权并更新本文。

## 当前配置

默认配置路径来自 `os.UserConfigDir()/neutron-star/model.json`。

支持的环境变量：

- `NEUTRON_ENDPOINT`
- `NEUTRON_API_KEY`
- `NEUTRON_MODEL`

配置错误不会阻止应用启动，而是通过 `ChatStatus.problem` 展示给用户。

## 原生窗口边界

- 窗口是无边框的，拖动区由 CSS `--wails-draggable: drag` 指定。
- 交互控件使用 `no-drag`。
- 顶栏只承载拖动、侧边栏切换和窗口控制。
- Linux 开发构建使用 GTK3 后端，原因见 [ADR-0003](decisions/0003-gtk3-dev-backend.md)。

## 受保护边界

`internal/**` 是生产代码区域，协作者只读。界面逻辑、Wails 桥接和文档修改不得顺手改变该目录。

接口行为需要变化时，应先更新当前契约和实现计划，再获得用户授权。当前契约可以被重构，但不能在没有迁移计划的情况下悄悄破坏现有界面。

## 验证层次

1. 类型检查：`pnpm typecheck`
2. 前端构建：`pnpm build:dev`
3. Go 测试：`go test -tags gtk3 ./...`
4. 真实窗口检查：运行 `wails3 dev`，验证布局、焦点、滚动、拖动和响应式行为
