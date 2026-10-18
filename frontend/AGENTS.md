# frontend/AGENTS.md

这些规则适用于 `frontend/` 下的前端工作，并与仓库根目录的 `AGENTS.md` 合并生效。

## 边界

- 不修改仓库根目录 `internal/**`。
- 不手工修改 `frontend/bindings/**`；它们由 Wails 生成。
- 不修改 `frontend/src/assets/fonts/` 中的字体文件，除非任务明确要求更换字体。
- 新增依赖前说明用途、体积和替代方案。

## 技术约束

- React 18 + TypeScript + Vite。
- Wails 运行时负责窗口、事件与原生调用。
- 当前对话流通过 `chat:delta`、`chat:done`、`chat:error` 事件更新；这是临时桥接契约，不是最终领域模型。
- 不在组件中直接拼接字体名称；使用 `--font-sans`、`--font-mono`。
- 交互元素必须提供可访问名称；不要只依赖图标或颜色表达状态。
- 保留窗口无边框前提下的拖动区与 `no-drag` 规则。

## 修改前阅读

- 全局视觉与交互：`docs/frontend/design-system.md`
- Wails 调用与事件：`docs/contracts/chat-service.md`
- 本地字体来源：`src/assets/fonts/README.md`

## 验证

```bash
cd frontend
pnpm typecheck
pnpm build:dev
```

涉及 UI 时还要：

1. 在运行的 `wails3 dev` 窗口中验证真实布局。
2. 检查展开/收起、悬停、窄窗口和空状态。
3. 检查控制台没有新的运行时错误。

## 样式原则

- 复用现有 CSS 变量和语义类名，避免复制整套颜色或尺寸值。
- 字体族只允许在 `src/styles/fonts.css` 定义。
- 以白色实色表面为基底，强调色只使用蓝色或紫色体系。
- 承载文字的区域不得使用透明背景、毛玻璃或半透明标签页。
- 不添加没有真实后端能力、真实状态或实际操作的僵尸控件。
- 简单可靠优先于装饰性；删除无用途入口，而不是用静态假数据占位。
- 只有顶栏空白区域可以拖动窗口；侧边栏和其他内容区域不得设置 `--wails-draggable: drag`。
- 页面框架默认不可选文本，只有对话正文、配置内容和输入编辑器开放选择。
- 720px 窄屏断点使用可开合侧栏抽屉；层级必须保持顶栏 > 侧栏 > 对话区/Composer。
- 右侧边栏当前是空白预留区域；未接入真实功能前不得放入示例数据或假控件。
- 对话列底部面板当前保持空白；未接入终端或任务视图前不得放入示例输出或假控件。
- 组件样式优先修改 `src/styles/style.css` 中的现有区块，不随意创建第二套设计系统。
- 动画需兼容 `prefers-reduced-motion`。
