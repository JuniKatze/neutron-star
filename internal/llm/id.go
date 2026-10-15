package llm

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// NewID 返回由密码学随机源生成的内部标识。
// 标识包含 16 字节随机值，编码为 32 个小写十六进制字符。
//
// NewID 不提供绝对唯一性保证；需要唯一性的使用处仍应检查冲突。
// 它不用于认证或权限裁决，也不包含时间、模型或供应商信息。
//
// NewID 返回读取器报告的错误，但无法处理随机源自身导致的进程终止。
// 使用标准默认随机源时，可以并发调用 NewID。
func NewID() (string, error) {
	return newIDFromReader(rand.Reader)
}

// newIDFromReader 从 reader 读取一个完整标识。
// 它允许测试注入读取器，不修改全局随机源。
// 读取失败时返回空标识，并保留底层错误。
func newIDFromReader(reader io.Reader) (string, error) {
	if reader == nil {
		return "", errors.New("generate id: random source is nil")
	}

	var value [16]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}

	return hex.EncodeToString(value[:]), nil
}
