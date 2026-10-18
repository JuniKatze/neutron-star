# ChatService 当前桥接契约

> **状态：Provisional**
>
> 本文只描述当前根目录 `service.go` 提供给 React 界面的桥接行为，不是最终
> `internal/**` 领域契约。方法、DTO、字段和状态所有权都可能在后续后端设计中迁移或重构。

TypeScript 类型由 Wails 生成到 `frontend/bindings/neutron-star/`。生成结果与本文都必须和当前桥接实现保持一致，但其存在不代表领域模型已经定稿。

当前实现入口：仓库根目录 `service.go`（临时脚手架）。

## 调用

### `Status(): ChatStatus`

返回当前连接和运行状态。

| 字段 | 含义 |
| --- | --- |
| `configured` | 适配器已创建，可以发送请求 |
| `running` | 当前有生成尚未结束 |
| `model` | 当前模型名 |
| `endpoint` | 当前请求端点 |
| `configPath` | 用户配置文件路径 |
| `problem` | 不能使用的原因；空字符串表示正常 |

前端挂载时调用一次，并在重新加载配置后再次调用。

### `Send(prompt, history)`

接受一次用户输入并启动生成。

- `prompt` 会去除首尾空白；空白输入同步返回错误。
- `history` 是此前的正文对话，不包含推理、工具调用和界面提示。
- 方法在校验完成并启动流之后返回，不等待完整回答。
- 生成内容通过事件返回。
- 同一时间只允许一次生成。

### `Stop()`

请求取消当前生成。没有生成时调用不会报错。

用户主动停止仍通过 `chat:error` 结束，载荷中的 `canceled` 为 `true`。

### `Reload()`

重新读取本地模型配置并返回新的 `ChatStatus`。

正在运行的生成仍使用启动时取得的适配器，不受重载影响。

## 数据类型

以下类型目前定义在根目录 `service.go`，主要承担 Wails 序列化和界面渲染职责。它们的包位置、字段划分和命名不是最终领域设计，后续可以迁入 `internal/**`。

### `ChatTurn`

```ts
{
    role: "user" | "assistant";
    text: string;
}
```

当前只承载正文。空白轮次会在当前桥接实现中被忽略，不进入本次模型请求。

### `ChatDelta`

```ts
{
    partIndex: number;
    kind: "text" | "reasoning" | "refusal" | "tool_call" | string;
    text?: string;
    toolName?: string;
}
```

- `partIndex` 表示该内容在最终消息中的顺序。
- `text` 是新增片段，不是完整文本。
- `tool_call` 当前只携带工具名；工具参数和执行尚未接入。

### `ChatOutcome`

```ts
{
    text?: string;
    stopReason?: string;
    error?: string;
    canceled?: boolean;
}
```

- 正常结束：`chat:done`，`text` 是完整正文。
- 失败：`chat:error`，`error` 说明原因；`text` 可能包含失败前已收到的正文。
- 用户停止：`chat:error`，`canceled: true`。

## 事件

| 事件 | 频率 | 载荷 |
| --- | --- | --- |
| `chat:delta` | 多次 | `ChatDelta` |
| `chat:done` | 每次生成最多一次 | `ChatOutcome` |
| `chat:error` | 每次生成最多一次 | `ChatOutcome` |

一次生成以 `chat:done` 或 `chat:error` 结束，不应同时出现两者。

## 前端状态机

```text
idle
  → Send 被接受
  → running
      ├─ chat:delta* → streaming
      ├─ chat:done   → complete
      └─ chat:error  → complete（取消）或 error（失败）
```

## 演进与迁移

- 当桥接类型迁入 `internal/**` 时，更新 Wails 生成绑定，并确认前端只依赖必要字段。
- 可以在根目录保留一层薄 DTO 映射；是否保留由迁移后的边界决定。
- 新增会话持久化、工具执行、权限或事件模型时，先明确这些状态由谁拥有，再修改本文。
- 不得把当前 DTO 直接当作未来数据库模型、Agent 状态机或权限模型。

## 当前实现约束

- 当前桥接层不保存会话历史；历史由前端在每次发送时传入。
- 当前实现不会把推理内容和工具提示放入下一轮历史。
- `Send` 返回成功之前，前端已创建对应的 assistant 占位消息。
- 结束事件必须清除 `running`，即使没有活跃 assistant 消息。
- 用户取消不应留下红色错误状态或持续阻止后续发送。
- 在桥接层尚未替换期间，修改方法签名、字段或事件语义必须同步更新本文和生成绑定说明。
