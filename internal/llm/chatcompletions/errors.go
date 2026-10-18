package chatcompletions

import "neutron-star/internal/llm"

// 本文件集中构造适配器返回的 llm.Error。
// 分类口径只有这一份：其它文件不直接拼装 llm.Error。

// invalidRequestError 报告调用方给出的请求无法编码。
func invalidRequestError(message string, cause error) error {
	return &llm.Error{
		Kind:    llm.ErrorInvalidRequest,
		Message: message,
		Cause:   cause,
	}
}

// unsupportedError 报告 IR 能表达、但本协议没有对应表达的东西。
func unsupportedError(message string) error {
	return &llm.Error{
		Kind:    llm.ErrorUnsupported,
		Message: message,
	}
}

// protocolError 报告供应商的响应或流违反协议。
func protocolError(message string, cause error) error {
	return &llm.Error{
		Kind:    llm.ErrorProtocol,
		Message: message,
		Cause:   cause,
	}
}

// transportError 报告 HTTP 传输层失败。
func transportError(message string, cause error) error {
	return &llm.Error{
		Kind:    llm.ErrorTransport,
		Message: message,
		Cause:   cause,
	}
}
