package chatcompletions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"neutron-star/internal/llm"
)

// 本文件负责 HTTP 交互本身：构造请求、分类失败。
// 它不解释响应内容——那是 stream.go 与 accumulate.go 的事。

// newRequest 构造一次流式 POST。
// 默认头先设置，用户 Headers 最后应用，因此可以覆盖它们。
func (c *Client) newRequest(
	ctx context.Context,
	body []byte,
) (*http.Request, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint.String(),
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")

	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	for name, value := range c.headers {
		request.Header.Set(name, value)
	}

	return request, nil
}

// errorFromResponse 把非 200 响应转换为 IR 错误。
//
// 状态码是权威分类依据；供应商报文的 message/code 只作为补充信息保留。
// 读取响应体失败不影响分类结果。
func errorFromResponse(
	response *http.Response,
	requestID string,
) error {
	body, _ := readErrorBody(response.Body, defaultMaxErrorBodyBytes)

	message := ""
	providerCode := ""
	providerType := ""

	var envelope wireErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil &&
		envelope.Error != nil {
		message = strings.TrimSpace(envelope.Error.Message)
		providerCode = codeText(envelope.Error.Code)
		providerType = strings.TrimSpace(envelope.Error.Type)
	} else if text := strings.TrimSpace(string(body)); text != "" {
		message = text
	}

	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	if message == "" {
		message = "request failed"
	}
	if providerCode == "" {
		providerCode = providerType
	}

	retryAfter, _ := parseRetryAfter(response.Header.Get("Retry-After"))

	return &llm.Error{
		Kind:              kindFromStatus(response.StatusCode),
		Message:           message,
		StatusCode:        response.StatusCode,
		ProviderCode:      providerCode,
		RequestID:         requestID,
		RetryAfterSeconds: retryAfter,
	}
}

// errorFromChunk 把流内报告的供应商错误转换为 IR 错误。
//
// 此时状态码已经是 200，只能依据供应商自己的错误类型分类；
// 无法识别时归类为 unavailable，不猜测具体原因。
func errorFromChunk(value *wireError, requestID string) error {
	message := strings.TrimSpace(value.Message)
	if message == "" {
		message = "provider reported an error in the stream"
	}

	return &llm.Error{
		Kind:         kindFromProviderError(value),
		Message:      message,
		StatusCode:   http.StatusOK,
		ProviderCode: codeText(value.Code),
		RequestID:    requestID,
	}
}

// kindFromStatus 按 HTTP 状态码分类。
func kindFromStatus(status int) llm.ErrorKind {
	switch status {
	case http.StatusBadRequest,
		http.StatusMethodNotAllowed,
		http.StatusConflict,
		http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity:
		return llm.ErrorInvalidRequest

	case http.StatusUnauthorized:
		return llm.ErrorAuthentication

	case http.StatusForbidden:
		return llm.ErrorPermissionDenied

	case http.StatusNotFound:
		return llm.ErrorNotFound

	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return llm.ErrorTimeout

	case http.StatusTooManyRequests:
		return llm.ErrorRateLimited
	}

	if status >= 500 {
		return llm.ErrorUnavailable
	}
	if status >= 400 {
		return llm.ErrorInvalidRequest
	}

	return llm.ErrorUnavailable
}

// kindFromProviderError 按供应商错误类型与错误码分类。
// 这是启发式判断：只有明确匹配才给出具体分类。
func kindFromProviderError(value *wireError) llm.ErrorKind {
	label := strings.ToLower(
		strings.TrimSpace(value.Type) + " " + codeText(value.Code),
	)

	switch {
	case strings.Contains(label, "rate_limit"):
		return llm.ErrorRateLimited

	case strings.Contains(label, "quota"):
		return llm.ErrorQuota

	case strings.Contains(label, "auth"), strings.Contains(label, "api_key"):
		return llm.ErrorAuthentication

	case strings.Contains(label, "permission"):
		return llm.ErrorPermissionDenied

	case strings.Contains(label, "not_found"):
		return llm.ErrorNotFound

	case strings.Contains(label, "invalid"),
		strings.Contains(label, "context_length"):
		return llm.ErrorInvalidRequest

	case strings.Contains(label, "overload"),
		strings.Contains(label, "unavailable"),
		strings.Contains(label, "server_error"):
		return llm.ErrorUnavailable
	}

	return llm.ErrorUnavailable
}

// parseRetryAfter 解析 Retry-After 的秒数或 HTTP 日期形式。
//
// 返回建议等待的秒数；无法解析或已经过期时返回零，
// 表示没有已知等待建议。HTTP 日期形式向上取整到秒，
// 避免"还差半秒"被报成零。
func parseRetryAfter(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0, false
		}

		return seconds, true
	}

	if moment, err := http.ParseTime(value); err == nil {
		delay := time.Until(moment)
		if delay <= 0 {
			return 0, false
		}

		return int64((delay + time.Second - 1) / time.Second), true
	}

	return 0, false
}

// classifyTransportError 把传输层失败归类为 IR 错误。
//
// 上下文错误优先；传输错误本身并进错误链，errors.Is/As 仍能看到它。
func classifyTransportError(ctx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return classifyContextError(errors.Join(err, ctx.Err()))

	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return classifyContextError(err)
	}

	return transportError("http transport failed", err)
}

// classifyContextError 把 context 失败归类为取消或超时。
//
// IR 层有同样的判据，但它未导出，适配器只能自己保留一份；
// 分类口径必须与 IR 一致，否则上层会看到两套"取消"。
func classifyContextError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return &llm.Error{
			Kind:    llm.ErrorCanceled,
			Message: "stream canceled",
			Cause:   err,
		}

	case errors.Is(err, context.DeadlineExceeded):
		return &llm.Error{
			Kind:    llm.ErrorTimeout,
			Message: "stream deadline exceeded",
			Cause:   err,
		}
	}

	return err
}

// readErrorBody 读取错误响应体，最多 limit 字节。
// 超出的部分被丢弃：错误报文只用于诊断，不构成协议内容。
func readErrorBody(reader io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(reader, limit))
}

// codeText 把供应商错误码的原始 JSON 还原为文本。
// 字符串、数字与缺失都能处理；其他形状返回原文。
func codeText(code json.RawMessage) string {
	trimmed := bytes.TrimSpace(code)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}

	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		return strings.TrimSpace(text)
	}

	return string(trimmed)
}
