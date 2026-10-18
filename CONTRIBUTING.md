# 贡献指南

## 开发流程

1. 先检查工作区状态：`git status --short`。
2. 确认改动边界，尤其是不要修改 `internal/**` 或生成绑定。
3. 做最小范围修改，并同步更新受影响的文档。
4. 运行与改动对应的检查。
5. 在提交说明中写明动机、修改范围、验证方式和已知限制。

## 环境准备

```bash
cd frontend
pnpm install
```

启动应用：

```bash
wails3 dev
```

## 必跑检查

前端：

```bash
cd frontend
pnpm typecheck
pnpm build:dev
```

Go：

```bash
go test -tags gtk3 ./...
```

UI 改动必须在真实的 Wails 窗口中验证，不能只检查浏览器预览或编译结果。

## 受保护内容

- `internal/**`：当前生产代码区域，协作者不得修改。
- `frontend/bindings/**`：由 Wails 生成，不手工编辑。
- 用户本地配置、API Key 和环境变量：不得提交。

## 文档同步

- 修改系统结构或数据流：更新 `docs/architecture.md`。
- 修改当前 Wails 调用或事件载荷：更新 `docs/contracts/chat-service.md`；若职责迁入 `internal/**`，同时更新架构文档和相关 ADR。
- 修改视觉规则、字体或窗口框架：更新 `docs/frontend/design-system.md`。
- 做出长期架构取舍：在 `docs/decisions/` 新增 ADR。

## 提交前检查

- 没有意外修改受保护目录。
- 没有包含秘密或本地绝对路径。
- 新旧功能路径均经过验证。
- 相关文档与实际行为一致。
