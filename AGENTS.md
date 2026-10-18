# AGENTS.md

## 项目说明

`neutron-star` 是一个使用 Go、Wails 3 和 React 开发的个人 Agent 桌面应用，仍处于开发阶段。

## 硬性边界

- 禁止修改、格式化、生成、移动或删除 `internal/**`。该目录是生产环境，只允许按任务需要读取。
- 禁止手工编辑 `frontend/bindings/**`。这些文件由 Wails 生成；需要更新时使用生成命令。
- 不覆盖工作区中已有的未提交改动。开始任务前先运行 `git status --short`。
- 不使用 `git reset --hard`、`git checkout --` 等会丢弃用户改动的操作，除非用户明确要求。
- 不把 API Key、令牌、用户配置或其他秘密写入仓库。
- 不修改与当前任务无关的文件，不顺便重构其他模块。

## 开始任务前

1. 阅读本文件；前端任务还必须阅读 `frontend/AGENTS.md`。
2. 根据任务阅读 `README.md`、`docs/architecture.md` 和相关契约/ADR。
3. 检查 `git status --short`，识别并保留已有改动。
4. 明确修改边界、验证方式和可能影响的用户流程。

## 代码结构

- `main.go`：临时 Wails 启动和窗口配置，不代表最终应用结构。
- `service.go`：临时前端桥接层，其中的方法和 DTO 后续可能迁入 `internal/**`。
- `frontend/src/`：React 应用、状态、组件和样式。
- `frontend/bindings/`：生成的 TypeScript API，不手工维护。
- `docs/`：架构、接口契约、设计系统和关键决策。
- `build/`：Wails 构建任务和平台配置。

不要把临时脚手架当成已经确定的领域模型，也不要用文档阻止后续把类型和职责迁入 `internal/**`。涉及桥接层重构时，先保留 UI 可用性，再同步更新契约和生成绑定。

## 验证要求

修改前端后至少运行：

```bash
cd frontend
pnpm typecheck
pnpm build:dev
```

修改 Go 代码后运行：

```bash
go test -tags gtk3 ./...
```

涉及界面布局、窗口行为或交互时，必须在实际的 `wails3 dev` 窗口中检查。静态构建通过不等同于界面完成。

## 完成标准

- 需求中的正常路径和明显边界状态都可用。
- 验证命令通过，并在最终回复中说明实际运行了什么。
- 对 UI 改动说明视觉或交互验证结果。
- 修改接口、架构或关键决策时同步更新相关文档。
- 明确说明没有修改 `internal/**`，除非用户另行授权。

## 文档地图

- 产品上手：`README.md`
- 系统结构：`docs/architecture.md`
- 当前 Wails 临时桥接契约：`docs/contracts/chat-service.md`
- 前端规范：`docs/frontend/design-system.md`
- 关键决策：`docs/decisions/`
- 协作流程：`CONTRIBUTING.md`
