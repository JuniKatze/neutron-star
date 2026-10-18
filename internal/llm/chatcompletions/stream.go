package chatcompletions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// 本文件解析 SSE 响应：只做"字节流 → wireChunk"这一段。
// 增量累计与最终结果装配在 accumulate.go。

const (
	// sseBufferBytes 是行缓冲的初始大小。
	sseBufferBytes = 64 << 10

	// sseDataField 是事件载荷字段名。
	sseDataField = "data:"

	// sseDone 是流正常结束的标记载荷。
	sseDone = "[DONE]"
)

// readStream 解析 SSE 响应，把每个 data 载荷交给 decode 与 consume。
//
// 返回 nil 表示流正常结束：可能是 [DONE]，也可能是对端关闭连接——后者
// 不代表接收成功，finish_reason 是否齐全由装配阶段判定。
// 返回错误表示读取失败、载荷非法或载荷超过大小上限。
//
// 本次协议不解析的字段（event、id、retry 与注释行）一律忽略。
// consume 返回 false 时立即停止读取并返回 nil。
func readStream(
	ctx context.Context,
	reader io.Reader,
	decode func([]byte) (*wireChunk, error),
	consume func(*wireChunk) bool,
) error {
	buffered := bufio.NewReaderSize(reader, sseBufferBytes)

	// payload 聚合一个事件的全部 data 行，行间用换行连接。
	var payload bytes.Buffer

	for {
		if err := ctx.Err(); err != nil {
			return classifyContextError(err)
		}

		line, err := readLine(buffered, defaultMaxSSEEventBytes)
		if err != nil && !errors.Is(err, io.EOF) {
			// 取消或超时也会中断阻塞读取，此时失败原因不是协议。
			if contextErr := ctx.Err(); contextErr != nil {
				return classifyContextError(contextErr)
			}

			return protocolError("read SSE stream", err)
		}

		atEnd := errors.Is(err, io.EOF)
		trimmed := bytes.TrimRight(line, "\r\n")

		switch {
		case len(trimmed) == 0:
			// 空行结束一个事件；keepalive 也会产生空行，此时没有载荷。
			if payload.Len() > 0 {
				done, err := dispatchEvent(
					payload.Bytes(),
					decode,
					consume,
				)
				payload.Reset()

				if err != nil {
					return err
				}
				if done {
					return nil
				}
			}

		case bytes.HasPrefix(trimmed, []byte(sseDataField)):
			value := bytes.TrimSpace(trimmed[len(sseDataField):])
			if payload.Len() > 0 {
				payload.WriteByte('\n')
			}

			payload.Write(value)

			if payload.Len() > defaultMaxSSEEventBytes {
				return protocolError(fmt.Sprintf(
					"SSE event exceeds %d bytes",
					defaultMaxSSEEventBytes,
				), nil)
			}
		}

		if !atEnd {
			continue
		}

		// 对端在事件结束前关闭：先处理已聚合的载荷。
		if payload.Len() > 0 {
			done, err := dispatchEvent(
				payload.Bytes(),
				decode,
				consume,
			)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}

		return nil
	}
}

// dispatchEvent 处理一个已聚合的 data 载荷。
// 返回 true 表示不需要继续读取（收到 [DONE]，或消费方要求停止）。
func dispatchEvent(
	payload []byte,
	decode func([]byte) (*wireChunk, error),
	consume func(*wireChunk) bool,
) (bool, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return false, nil
	}
	if bytes.Equal(trimmed, []byte(sseDone)) {
		return true, nil
	}

	chunk, err := decode(trimmed)
	if err != nil {
		return false, protocolError("decode stream chunk", err)
	}

	return !consume(chunk), nil
}

// readLine 读取一行，保留行尾换行符。
//
// 单行超过 limit 时返回错误，避免损坏或恶意的流耗尽内存。
// 对端关闭时返回已读到的部分与 io.EOF。
func readLine(reader *bufio.Reader, limit int) ([]byte, error) {
	line := make([]byte, 0, 256)

	for {
		chunk, err := reader.ReadSlice('\n')
		line = append(line, chunk...)

		if len(line) > limit {
			return nil, fmt.Errorf("line exceeds %d bytes", limit)
		}

		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}

		return line, err
	}
}

// decodeChunk 把一个 SSE 载荷解码为 chunk。
// 接收时宽容：未知字段一律忽略。
func (c *Client) decodeChunk(payload []byte) (*wireChunk, error) {
	var chunk wireChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return nil, err
	}

	// 默认字段名已由 wireDelta 覆盖；自定义字段名需要在原始对象上再取
	// 一次，因为结构体标签无法随配置变化。
	if c.reasoningField == "" ||
		c.reasoningField == defaultReasoningField {
		return &chunk, nil
	}

	if err := extractReasoning(payload, c.reasoningField, &chunk); err != nil {
		return nil, err
	}

	return &chunk, nil
}

// extractReasoning 从原始载荷中取出自定义名称的推理字段。
func extractReasoning(
	payload []byte,
	field string,
	chunk *wireChunk,
) error {
	var probe struct {
		Choices []struct {
			Delta map[string]json.RawMessage `json:"delta"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(payload, &probe); err != nil {
		return err
	}

	for index := range probe.Choices {
		if index >= len(chunk.Choices) {
			break
		}
		if chunk.Choices[index].Delta == nil {
			continue
		}

		value, exists := probe.Choices[index].Delta[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}

		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return fmt.Errorf("delta.%s: %w", field, err)
		}

		chunk.Choices[index].Delta.ReasoningContent = &text
	}

	return nil
}
