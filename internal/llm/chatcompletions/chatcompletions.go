package chatcompletions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"neutron-star/internal/llm"
)

// 本文件是适配器的入口：构造实例、冻结请求、驱动一次接收。
// 请求怎么编码在 encode.go，HTTP 怎么发在 http.go，
// 字节流怎么解析在 stream.go，结果怎么装配在 accumulate.go。

// Client 是一个 Chat Completions 适配器实例。
//
// 它在构造期读一遍 Options，之后不再引用调用方的配置对象。
// 所有字段在构造后只读，实例本身不保存调用状态：请求快照、流状态与
// HTTP 连接都活在单次 Generate 里，因此可以并发调用。
type Client struct {
	endpoint       *url.URL
	apiKey         string
	httpClient     HTTPDoer
	headers        map[string]string
	includeUsage   bool
	extraBody      map[string]json.RawMessage
	maxTokensParam string
	reasoningField string
	logger         *slog.Logger
}

// 编译期确认适配器满足 IR 契约。
var _ llm.LLM = (*Client)(nil)

// New 构造一个适配器实例。
//
// 选项错误在这里立即返回：地址写错、字段名写错属于启动期就该修的问题，
// 推迟到每次生成才报只会把配置事故变成运行事故。
//
// 返回的实例可以并发使用。构造成功后调用方可以随意修改传入的 Options。
func New(options Options) (*Client, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}

	endpoint, err := url.Parse(options.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("endpoint: %w", err)
	}

	client := &Client{
		endpoint:       endpoint,
		apiKey:         options.APIKey,
		httpClient:     options.HTTPClient,
		includeUsage:   options.IncludeUsage,
		maxTokensParam: options.MaxTokensParam,
		logger:         options.Logger,
	}

	// 不设客户端总超时：流式响应可能长时间没有数据，超时交给 ctx。
	if client.httpClient == nil {
		client.httpClient = &http.Client{}
	}

	// 复制可变对象：调用方之后修改自己的 map 不应改变已构造的实例。
	if options.Headers != nil {
		client.headers = make(
			map[string]string,
			len(options.Headers),
		)
		for name, value := range options.Headers {
			client.headers[name] = value
		}
	}
	if options.ExtraBody != nil {
		client.extraBody = make(
			map[string]json.RawMessage,
			len(options.ExtraBody),
		)
		for key, value := range options.ExtraBody {
			client.extraBody[key] = append(
				json.RawMessage(nil),
				value...,
			)
		}
	}

	// 只有显式声明支持时才启用推理字段。
	// 字段名已由 validate 补默认值并检查过。
	if options.SupportsAssistantReasoning {
		client.reasoningField = options.AssistantReasoningField
	}

	return client, nil
}

// Generate 开始一次生成。
//
// 它先冻结请求：深拷贝、校验、编码。之后适配器不再读调用方的内存，
// 因此 Generate 返回后调用方可以自由修改或复用 req。
//
// HTTP 连接与响应体在首次消费时才建立：一份从未被消费的返回流不会
// 产生任何网络副作用。非法请求、能力不支持与传输失败都作为流中的
// error 报告，不返回 nil 流。
func (c *Client) Generate(
	ctx context.Context,
	req *llm.Request,
) llm.ValidatedStream {
	frozen, err := c.freeze(req)
	if err != nil {
		return llm.GuardStream(ctx, failedStream(err), nil)
	}

	resources := &streamResources{}

	raw := func(yield func(*llm.StreamEvent, error) bool) {
		c.pump(ctx, frozen, resources, yield)
	}

	// GuardStream 负责单次消费、生命周期检查与 final 与 delta 的一致性；
	// cleanup 保证取消与关闭响应体在正常结束、报错、提前退出时都发生。
	return llm.GuardStream(ctx, llm.Stream(raw), resources.release)
}

// frozenRequest 是一次调用的请求快照及其编码结果。
//
// 它在本次调用内只读；快照在 Generate 返回前完成，因此编码只做一次，
// 不随流的推进重建。
type frozenRequest struct {
	// request 是深拷贝后的请求，交付 final 前要用它做 ValidateFor。
	request *llm.Request

	// body 是冻结的请求体字节；每次消费直接发出去。
	body []byte
}

// freeze 深拷贝请求、校验并编码为请求体。
//
// 返回的错误都是 llm.Error：非法请求归 invalid_request，
// 本协议没有对应字段的选项归 unsupported。
func (c *Client) freeze(request *llm.Request) (*frozenRequest, error) {
	if request == nil {
		return nil, invalidRequestError("request is nil", nil)
	}

	frozen := request.Clone()
	if err := frozen.Validate(); err != nil {
		return nil, invalidRequestError("invalid request", err)
	}

	body, err := c.buildBody(frozen)
	if err != nil {
		return nil, err
	}

	return &frozenRequest{request: frozen, body: body}, nil
}

// streamResources 保存一次接收在消费期间创建的资源。
//
// 只有取得消费权的 goroutine 会访问它，因此不需要加锁。
// release 作为 GuardStream 的 cleanup 至多执行一次；写成幂等是为了
// 让"资源已释放"这件事不依赖调用方的调度。
type streamResources struct {
	cancel context.CancelFunc
	body   io.ReadCloser
}

// release 关闭响应体，并在子 context 上取消。
//
// 两者都要做：取消让仍在阻塞的读取立刻失败，关闭确保连接不被挂在
// 连接池里。顺序不影响结果。
func (r *streamResources) release() {
	if r.body != nil {
		_ = r.body.Close()
		r.body = nil
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

// pump 执行一次完整的接收：建连、解析、装配、交付 final。
//
// 它按顺序尝试，任何一步失败都交付一个错误后立即返回：
// 流上不会出现"错误之后再成功"的假象。yield 返回 false 表示下游
// 已经停止消费，此时也直接返回。
func (c *Client) pump(
	ctx context.Context,
	frozen *frozenRequest,
	resources *streamResources,
	yield func(*llm.StreamEvent, error) bool,
) {
	// 子 context 让"取消本次接收"与"释放响应体"绑在一起：
	// cleanup 取消它，正在进行中的阻塞读取随即返回。
	child, cancel := context.WithCancel(ctx)
	resources.cancel = cancel

	request, err := c.newRequest(child, frozen.body)
	if err != nil {
		yield(nil, transportError("build http request", err))
		return
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		yield(nil, classifyTransportError(child, err))
		return
	}

	// 无论状态码如何，响应体都交给 cleanup 关闭。
	resources.body = response.Body

	requestID := responseRequestID(response.Header)

	if response.StatusCode != http.StatusOK {
		yield(nil, errorFromResponse(response, requestID))
		return
	}

	c.receive(child, response.Body, requestID, frozen, yield)
}

// receive 解析 200 流式响应并把增量与最终结果交付给上游。
//
// 它负责三件事的先后关系：先交付 delta，再装配 final，
// 最后用冻结的请求校验 final，通过后才交付。
func (c *Client) receive(
	ctx context.Context,
	body io.Reader,
	requestID string,
	frozen *frozenRequest,
	yield func(*llm.StreamEvent, error) bool,
) {
	accumulator := newAccumulator(c.reasoningField)

	// streamErr 保存消费回调里产生的错误：回调只能返回"是否继续"，
	// 因此错误先记下来，等解析停下后再交付。
	var streamErr error
	stopped := false

	consume := func(chunk *wireChunk) bool {
		// 200 之后供应商仍可能在流内报错；此时没有状态码可用，
		// 只能按它自己声明的错误类型分类。
		if chunk.Error != nil {
			streamErr = errorFromChunk(chunk.Error, requestID)
			return false
		}

		deltas, err := accumulator.accept(chunk)
		if err != nil {
			streamErr = err
			return false
		}

		for _, delta := range deltas {
			event, err := llm.NewDelta(*delta)
			if err != nil {
				streamErr = protocolError(
					"assembled delta is invalid", err,
				)
				return false
			}

			if !yield(event, nil) {
				stopped = true
				return false
			}
		}

		return true
	}

	if err := readStream(ctx, body, c.decodeChunk, consume); err != nil {
		yield(nil, err)
		return
	}

	if streamErr != nil {
		yield(nil, classifyStreamError(streamErr))
		return
	}

	if stopped {
		// 下游已经停止消费，不再产出 final。
		return
	}

	final, err := accumulator.response(requestID)
	if err != nil {
		yield(nil, classifyStreamError(err))
		return
	}

	// 交付前先对冻结的请求做结构校验：结果的工具调用 ID 不能与
	// 请求历史里的调用冲突。这一步不检查工具是否已声明、能否执行。
	if err := final.ValidateFor(frozen.request); err != nil {
		yield(nil, protocolError(
			"assembled response conflicts with the request", err,
		))
		return
	}

	event, err := llm.NewFinal(final)
	if err != nil {
		yield(nil, protocolError("assembled response is invalid", err))
		return
	}

	yield(event, nil)
}

// failedStream 返回只报告一个错误的原始流。
//
// 用于 Generate 无法开始接收的情况（请求非法、编码失败）：
// 契约要求错误走流，因此这里不返回 nil 流，由 GuardStream 统一包装。
func failedStream(err error) llm.Stream {
	return func(yield func(*llm.StreamEvent, error) bool) {
		yield(nil, err)
	}
}

// responseRequestID 读取响应头里的请求标识。
//
// 它只在诊断时有价值：报错时把它带给上层，便于对着供应商日志定位。
// x-request-id 是 OpenAI 与多数兼容实现的头名，request-id 是部分
// 网关的写法；都没有时返回空字符串。
func responseRequestID(header http.Header) string {
	if value := strings.TrimSpace(header.Get("x-request-id")); value != "" {
		return value
	}

	return strings.TrimSpace(header.Get("request-id"))
}
