package llm

import (
	"context"
	"sync"
	"sync/atomic"
)

// GuardStream 将 raw 包装为受保护的单次消费流。
// delta 立即交付，final 暂存至 raw 正常返回后交付。
//
// GuardStream 拒绝无 final 的结束、重复 final、final 后事件、
// 无效事件及同时携带事件和错误的输出。
// 接收的事件会被深拷贝，避免上游后续修改影响消费者。
//
// 消费开始后，cleanup 在正常结束、错误、提前退出及栈展开时
// 至多执行一次。cleanup 为 nil 时不执行清理操作。
// 若流从未消费，则 raw 和 cleanup 均不执行。
// 消费退出时清空流自身对 raw、cleanup 和 ctx 的捕获引用，
// 避免已消费的流句柄继续持有请求或上下文对象；外部引用不受影响。
//
// raw 必须遵守 yield=false 后停止，并使用 ctx 中断阻塞 I/O。
// GuardStream 不创建读取 goroutine，也不能替 raw 中断不响应
// context 的阻塞操作。panic 不会被转换成模型错误。
func GuardStream(
	ctx context.Context,
	raw Stream,
	cleanup func(),
) ValidatedStream {
	var consumed atomic.Bool
	var cleanupOnce sync.Once

	release := func() {
		cleanupOnce.Do(func() {
			if cleanup != nil {
				cleanup()
			}
		})
	}

	return ValidatedStream(
		func(yield func(*StreamEvent, error) bool) {
			if !consumed.CompareAndSwap(false, true) {
				yield(nil, protocolError(
					"validated stream may only be consumed once",
					nil,
				))
				return
			}

			// 只有获得消费权的调用会访问这些捕获变量；重复消费者已在上方返回。
			// cleanup 即使发生 panic，也要清空引用，避免保留已消费流句柄时
			// 连带保留请求快照、HTTP 闭包或 context 引用的对象。
			defer func() {
				defer func() { raw = nil; cleanup = nil; ctx = nil }()
				release()
			}()

			if ctx == nil {
				yield(nil, &Error{
					Kind:    ErrorInvalidRequest,
					Message: "context is nil",
				})
				return
			}

			if raw == nil {
				yield(nil, protocolError(
					"raw stream is nil",
					nil,
				))
				return
			}

			if err := ctx.Err(); err != nil {
				yield(nil, contextStreamError(err))
				return
			}

			state := streamState{}
			downstreamStopped := false

			raw(func(event *StreamEvent, err error) bool {
				if downstreamStopped {
					return false
				}

				return state.step(
					ctx,
					event,
					err,
					func(delta *StreamEvent) bool {
						if yield(delta, nil) {
							return true
						}

						downstreamStopped = true
						return false
					},
				)
			})

			if downstreamStopped {
				return
			}

			// 合法 final 后 raw 正常返回，表示这次接收已完成。
			// 上游因取消而终止读取时，必须明确报告 error。
			if err := ctx.Err(); err != nil &&
				state.terminal == nil &&
				state.final == nil {
				state.terminal = contextStreamError(err)
			}

			final, err := state.finish()
			if err != nil {
				yield(nil, err)
				return
			}

			yield(&StreamEvent{
				Kind:  StreamFinal,
				Final: final,
			}, nil)
		},
	)
}

// Consume 消费 stream，并在完整成功后返回最终 Response。
// stream 为 nil 时返回协议错误。
//
// preview 对每个 delta 同步调用；preview 为 nil 时忽略预览。
// preview 返回错误时立即停止上游，返回该错误和 nil Response。
// preview 可以保留或修改收到的增量，不会修改内部累计状态。
//
// Consume 再次检查生命周期及增量一致性，避免错误实现或显式
// 类型转换绕过 ValidatedStream 的契约。
// 本函数不校验工具执行策略，也不将失败的预览当作成功结果返回。
func Consume(
	stream ValidatedStream,
	preview func(*PartDelta) error,
) (*Response, error) {
	if stream == nil {
		return nil, protocolError(
			"validated stream is nil",
			nil,
		)
	}

	state := streamState{}
	var previewErr error

	stream(func(event *StreamEvent, err error) bool {
		if previewErr != nil {
			return false
		}

		return state.step(
			nil,
			event,
			err,
			func(delta *StreamEvent) bool {
				if preview == nil {
					return true

				}

				if err := preview(delta.Delta); err != nil {
					previewErr = err
					return false
				}

				return true

			},
		)

	})

	if previewErr != nil {
		return nil, previewErr
	}

	return state.finish()
}
