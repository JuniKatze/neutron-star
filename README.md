# neutron-star

一个使用 Go、Wails 3 和 React 构建的本地 AI 工作台。当前版本聚焦于模型连接、流式对话和桌面界面基础能力。

项目仍处于开发阶段，接口和界面都可能继续调整。

> **实现状态说明**：`main.go` 和 `service.go` 是界面开发阶段形成的临时脚手架。其中的窗口配置、服务方法和 DTO 可能继续迁移到 `internal/**`，不代表最终领域模型。相关文档会明确区分“当前实现”和“目标架构”。

## 当前能力

- 无边框原生窗口，自绘顶栏、拖动区和窗口控制按钮。
- 支持 Markdown、代码块、推理内容和流式回答。
- 当前脚手架由前端保存对话历史，并在每次请求时显式发送给根目录桥接服务。
- 模型连接通过本地配置文件或环境变量配置。
- 本地打包黑体与 Maple Mono 等宽字体。
- 左栏可在桌面宽度平滑收起，在窄窗口中作为浮层打开。
- 对话列底部提供默认展开的空白工具面板，可折叠并供后续终端或任务输出使用。

## 技术栈

- Go 1.25
- Wails 3 `v3.0.0-beta.29`
- React 18、TypeScript、Vite
- CodeMirror Markdown 输入
- react-markdown + GFM

## 快速开始

### 环境要求

- Go 1.25 或更高版本
- Node.js 与 pnpm 12
- Wails 3 CLI
- Linux 开发还需要 GTK3 与 WebKitGTK 4.1 开发包

确认工具可用：

```bash
go version
pnpm --version
wails3 version
```

安装前端依赖：

```bash
cd frontend
pnpm install
```

启动开发环境：

```bash
wails3 dev
```

也可以在仓库根目录运行：

```bash
task dev
```

Linux 开发构建通过 `build/config.yml` 自动附加 `gtk3` 标签，原因见
[ADR-0003](docs/decisions/0003-gtk3-dev-backend.md)。

## 模型配置

默认配置文件位于用户配置目录：

```text
<UserConfigDir>/neutron-star/model.json
```

Linux 上通常是：

```text
~/.config/neutron-star/model.json
```

示例：

```json
{
  "endpoint": "https://api.example.com/v1/chat/completions",
  "apiKey": "sk-...",
  "model": "example-model",
  "includeUsage": false
}
```

环境变量会覆盖配置文件中的同名字段：

- `NEUTRON_ENDPOINT`
- `NEUTRON_API_KEY`
- `NEUTRON_MODEL`

不要把真实 API Key 提交到仓库。

## 常用检查

前端类型检查：

```bash
cd frontend
pnpm typecheck
```

前端开发构建：

```bash
cd frontend
pnpm build:dev
```

Go 测试：

```bash
go test -tags gtk3 ./...
```

## 项目结构

```text
frontend/                 React 界面
  src/chat/               对话状态与 Wails 事件桥接
  src/components/         界面组件
  src/styles/             字体与全局样式
  bindings/               Wails 生成的 TypeScript 绑定
internal/                 生产代码，默认禁止修改
service.go                临时前端桥接层，后续可能重构
main.go                   临时 Wails 启动与窗口配置
build/                    构建配置与平台任务
docs/                     架构、契约和设计文档
```

更完整的系统说明见 [架构文档](docs/architecture.md)。

## 当前限制

- `internal/llm` 是受保护的生产代码区域，协作时不得修改。
- `main.go`、`service.go` 及其中的 DTO 是临时实现，不是已经冻结的领域设计。
- 当前会话只保存在内存中，应用重启后不会恢复。
- 工具调用只展示请求提示，尚未执行具体工具。
- Linux 开发暂时使用 GTK3 后端，以规避 Wails beta 版本的 GTK4 拖动坐标问题。

## AI 协作

- 根目录 [AGENTS.md](AGENTS.md) 定义全仓库工作边界。
- [frontend/AGENTS.md](frontend/AGENTS.md) 定义前端开发规则。
- [docs/](docs/) 提供架构、协议和设计上下文。
- [CONTRIBUTING.md](CONTRIBUTING.md) 描述修改、验证和提交要求。
