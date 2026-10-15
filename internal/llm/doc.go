// Package llm 定义供应商无关的模型生成契约：请求、消息、内容块、响应与流。
//
// 这个包只描述"生成结果如何交付"，不出现任何供应商的字段名或协议细节。
// 协议翻译由适配器负责（例如 Chat Completions 的适配器在子包里）。
//
// # 类型地图
//
// Request 描述一次生成：目标模型、消息上下文、可用的工具声明与生成选项。
// Message 保存有序的 Part；Part 是按 Kind 取值的判别联合，文本、推理、拒绝、
// JSON、工具调用、媒体与工具结果都在其中。Response 保存一次完整接收的结果，
// 附带归一化的 Usage 与供应商元数据。
//
// Config、Reasoning、ToolChoice、ResponseFormat 与 ToolSchema 是请求侧的声明；
// Source、Image、Audio、Video、Document、File 描述内容来源；Replay 保存适配器
// 私有的回放状态，核心层不解释它的内容。
//
// # 校验分工
//
// 各类型的 Validate 只检查结构与本层契约：不裁剪字符串、不补默认值、不执行
// 工具、不获取资源，也不判断目标供应商是否支持。可选配置与指针字段用 nil
// 表示未设置；非 nil 时的零值是否合法，由各类型自己的注释规定。
//
// 跨对象的关联分成两处：请求内部的历史关系由 Request.Validate 检查（调用必须
// 先闭合、ToolChoice 引用的工具必须已声明、system 消息构成连续前缀）；响应与
// 请求是否自洽由 Response.ValidateFor 检查（新工具调用不得与历史冲突）。
//
// # 流
//
// 一次生成通过流交付：delta 只供预览，final 才是完整结果。适配器必须用
// GuardStream 包装原始流，调用方用 Consume 取得最终结果。所有失败——包括
// nil ctx 与非法请求——都作为流中的 error 报告，不会伪装成事件。
//
// # 回放
//
// Replay 保存"必须原样带回、但核心层不理解"的供应商私有状态（例如推理签名）。
// ReplayScope 决定它属于哪条路由，换适配器、连接或模型后即失效；Binding 是
// 所属对象公开内容的摘要，用来发现内容被改过或私有状态被贴错对象——它不提供
// 真实性认证。
//
// # 现状
//
// 目前没有"外部 JSON → 核心对象"的解码入口：DecodeJSON 与 EncodeJSON 已被删除。
// jsonx.go 只提供两个外部 JSON 检查（validateValue、validateObject）与内容摘要
// （digest），且都不导出。需要从磁盘或前端读回核心对象时，当前只能自己
// json.Unmarshal 再调用 Validate——未知字段与重复键不会被拦下。
package llm
