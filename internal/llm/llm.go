package llm

import "context"

// LLM 是模型生成的接口，由各协议的适配器实现；上层只依赖它，不依赖供应商。
//
// 一次调用就是一次生成：Generate 接收冻结的请求，返回一条受保护的流。
// 流里的 delta 只供预览，final 才是完整结果。
//
// # 调用方的义务
//
//   - Generate 之后不得再修改 req，也不得在调用期间并发修改它：
//     实现会在返回前复制一份请求快照。
//   - 用 ctx 取消；阻塞读取必须响应取消。
//   - 需要最终结果时用 Consume，不要自己在流上判断"是否已经结束"。
//
// # 实现方的义务
//
//   - 返回的流必须由 GuardStream 包装，并且只能消费一次。
//   - 调用时立即冻结输入（深拷贝请求、完成编码），之后不得再读调用方的内存。
//   - 网络请求与相关资源在首次消费时创建；所有退出路径都必须释放资源。
//   - 交付候选 final 之前，先对冻结的请求调用 Response.ValidateFor。
//   - nil ctx、非法请求、能力不支持、传输失败一律作为流中的 error 报告：
//     不返回 nil 流，也不因非法输入 panic。
//
// 实现必须允许多个 goroutine 并发调用：各次调用的请求快照、流状态与网络
// 资源不得共享可变内存。
type LLM interface {
	// Generate 返回非 nil、只能消费一次的受保护流。
	Generate(ctx context.Context, req *Request) ValidatedStream
}
