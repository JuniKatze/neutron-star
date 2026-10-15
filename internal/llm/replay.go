package llm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ReplayScope 指定私有回放状态所属的路由：适配器、连接和模型。
//
// 标识按照原值精确比较，不裁剪，大小写敏感，三个字段都必须非空。
// 它的用途是“不解析 Data 也能判断这份私有状态是否继续可用”：
// 更换适配器、连接或模型后，供应商私有状态将不再可用。
type ReplayScope struct {
	// Adapter 是适配器的稳定标识。
	Adapter string `json:"adapter"`

	// Connection 是连接的稳定标识，不是连接凭证。
	Connection string `json:"connection"`

	// Model 是回放绑定的模型标识。
	Model string `json:"model"`
}

// Validate 检查三个标识是否非空。
// 本方法不修改 s，也不检查标识是否已注册。
func (s ReplayScope) Validate() error {
	if strings.TrimSpace(s.Adapter) == "" {
		return errors.New("adapter: required")
	}

	if strings.TrimSpace(s.Connection) == "" {
		return errors.New("connection: required")
	}

	if strings.TrimSpace(s.Model) == "" {
		return errors.New("model: required")
	}

	return nil
}

// Replay 保存适配器私有的状态。
//
// nil Replay 表示未设置；非 nil Replay 的零值无效。
type Replay struct {
	// Scope 指定这份私有状态所属的路由。
	Scope ReplayScope `json:"scope"`

	// Binding 是所属对象公开内容的摘要。
	Binding string `json:"binding"`

	// Data 保存非 null 的完整 JSON 值。
	Data json.RawMessage `json:"data"`
}

// Validate 检查 Scope、Binding 格式和 Data 的 JSON 结构。
// r 为 nil 时返回 nil。本方法不修改 r。
func (r *Replay) Validate() error {
	if r == nil {
		return nil
	}

	if err := r.Scope.Validate(); err != nil {
		return fmt.Errorf("scope: %w", err)
	}

	if err := validateReplayBinding(r.Binding); err != nil {
		return fmt.Errorf("binding: %w", err)
	}

	if len(r.Data) == 0 {
		return errors.New("data: required")
	}

	if err := validateValue(r.Data); err != nil {
		return fmt.Errorf("data: %w", err)
	}

	if bytes.Equal(bytes.TrimSpace(r.Data), []byte("null")) {
		return errors.New("data: must not be null")
	}

	return nil
}

// ValidateFor 检查 r，并要求它属于给定路由且绑定给定内容摘要。
// r 为 nil 时返回错误：需要回放时私有状态是必需的。
func (r *Replay) ValidateFor(
	scope ReplayScope,
	binding string,
) error {
	if r == nil {
		return errors.New("replay is required")
	}
	if err := r.Validate(); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return fmt.Errorf("target scope: %w", err)
	}
	if err := validateReplayBinding(binding); err != nil {
		return fmt.Errorf("target binding: %w", err)
	}

	if r.Scope != scope {
		return &Error{
			Kind:    ErrorUnsupported,
			Message: "replay scope does not match target",
		}
	}
	if r.Binding != binding {
		return &Error{
			Kind:    ErrorProtocol,
			Message: "replay binding does not match owner",
		}
	}

	return nil
}

// Clone 返回 r 的深拷贝。
// r 为 nil 时返回 nil。
func (r *Replay) Clone() *Replay {
	if r == nil {
		return nil
	}

	out := *r

	if r.Data != nil {
		out.Data = make(json.RawMessage, len(r.Data))
		copy(out.Data, r.Data)
	}

	return &out
}

// validateReplayBinding 检查 Binding 的前缀、长度及小写十六进制格式。
func validateReplayBinding(binding string) error {
	if !strings.HasPrefix(binding, "sha256:") {
		return errors.New("expected sha256 digest")
	}

	digest := strings.TrimPrefix(binding, "sha256:")
	if len(digest) != sha256.Size*2 {
		return errors.New("expected 64 hexadecimal digits")
	}
	if digest != strings.ToLower(digest) {
		return errors.New("digest must use lowercase hexadecimal")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return fmt.Errorf("invalid hexadecimal digest: %w", err)
	}

	return nil
}
