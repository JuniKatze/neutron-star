package llm

import (
	"context"
	"errors"
	"fmt"
)

// ErrorKind 表示跨供应商的错误分类。
// 分类用于上层决策，不表示请求一定可以安全重试。
type ErrorKind string

const (
	// ErrorInvalidRequest 表示请求结构或参数不合法。
	ErrorInvalidRequest ErrorKind = "invalid_request"

	// ErrorUnsupported 表示目标实现无法表达或支持请求的能力。
	ErrorUnsupported ErrorKind = "unsupported"

	// ErrorAuthentication 表示凭证缺失、无效或已失效。
	ErrorAuthentication ErrorKind = "authentication"

	// ErrorPermissionDenied 表示请求缺少所需访问权限。
	ErrorPermissionDenied ErrorKind = "permission_denied"

	// ErrorNotFound 表示目标资源不存在。
	ErrorNotFound ErrorKind = "not_found"

	// ErrorRateLimited 表示请求受到速率限制。
	ErrorRateLimited ErrorKind = "rate_limited"

	// ErrorQuota 表示额度、余额或配额限制。
	ErrorQuota ErrorKind = "quota"

	// ErrorUnavailable 表示目标服务暂时不可用。
	ErrorUnavailable ErrorKind = "unavailable"

	// ErrorTransport 表示连接或数据传输失败。
	ErrorTransport ErrorKind = "transport"

	// ErrorProtocol 表示响应结构或流生命周期违反协议。
	ErrorProtocol ErrorKind = "protocol"

	// ErrorTimeout 表示请求超时。
	ErrorTimeout ErrorKind = "timeout"

	// ErrorCanceled 表示请求被取消。
	ErrorCanceled ErrorKind = "canceled"
)

// Error 保存模型请求、传输或协议错误。
// 它与工具结果中的 ToolError 分开。
// Error 的零值可以格式化，但没有具体错误分类。
//
// Cause 不参与 JSON 编码。Unwrap 保留底层错误，
// 支持 errors.Is 和 errors.As。
//
// Error 创建后应视为只读；调用方不得在读取错误时并发修改字段。
type Error struct {
	// Kind 是供程序判断的错误分类。
	Kind ErrorKind `json:"kind"`

	// Message 是可选的错误说明。
	// 它优先于 Cause 用于格式化错误文本。
	Message string `json:"message,omitempty"`

	// StatusCode 是供应商或网关返回的 HTTP 状态码。
	// 零表示未提供状态码。
	StatusCode int `json:"statusCode,omitempty"`

	// ProviderCode 是供应商报告的原始错误代码。
	ProviderCode string `json:"providerCode,omitempty"`

	// RequestID 是供应商或网关报告的请求标识。
	RequestID string `json:"requestId,omitempty"`

	// RetryAfterSeconds 是建议等待的秒数。
	// 零表示没有已知等待建议，不表示应该立即重试；负值无效。
	RetryAfterSeconds int64 `json:"retryAfterSeconds,omitempty"`

	// Cause 保存底层错误，不参与 JSON 编码。
	Cause error `json:"-"`
}

// Error 返回 e 的诊断文本。
// e 为 nil 时返回 "<nil>"。
//
// 非空 Message 优先于 Cause，但不会影响 Unwrap。
// 本方法不修改 e，不将状态码、请求标识或等待时间追加到文本中。
// 调用方应使用字段判断分类，不应解析返回文本。
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	label := "llm"
	if e.Kind != "" {
		label += " " + string(e.Kind)
	}

	if e.Message != "" {
		return label + ": " + e.Message
	}

	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", label, e.Cause)
	}

	if e.Kind == "" {
		return "llm error"
	}

	return label
}

// Validate 检查错误分类、状态码与等待秒数。
// e 为 nil 时返回错误。本方法不修改 e，也不检查 Cause。
//
// 空 Kind 表示尚未分类；本包不锁定 Kind 词表，适配器可以报告
// 供应商私有分类。StatusCode 为零表示未提供。
func (e *Error) Validate() error {
	if e == nil {
		return errors.New("error is nil")
	}
	if e.StatusCode < 0 {
		return errors.New("statusCode: must not be negative")
	}
	if e.RetryAfterSeconds < 0 {
		return errors.New("retryAfterSeconds: must not be negative")
	}

	return nil
}

// Unwrap 返回 e 的底层错误。
// e 为 nil 或没有底层错误时返回 nil。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// protocolError 构造协议错误，并保留可选的底层原因。
func protocolError(message string, cause error) *Error {
	return &Error{
		Kind:    ErrorProtocol,
		Message: message,
		Cause:   cause,
	}
}

// contextStreamError 将 context 取消或超时错误转换为流错误。
// err 为 nil 时返回 nil，其他错误原样返回。
//
// 本函数识别被包装的 context 错误，并保留原始错误链。
// 如果同时匹配取消与超时，则优先归类为取消。
func contextStreamError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, context.Canceled):
		return &Error{
			Kind:    ErrorCanceled,
			Message: "stream canceled",
			Cause:   err,
		}

	case errors.Is(err, context.DeadlineExceeded):
		return &Error{
			Kind:    ErrorTimeout,
			Message: "stream deadline exceeded",
			Cause:   err,
		}

	default:
		return err
	}
}
