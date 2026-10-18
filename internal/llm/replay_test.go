package llm

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// replayTestScope 返回测试用的作用域。
func replayTestScope() ReplayScope {
	return ReplayScope{Adapter: "a", Connection: "c", Model: "m"}
}

// replayTestBinding 返回一个格式合法的摘要。
func replayTestBinding() string {
	return "sha256:" + strings.Repeat("a", 64)
}

// TestReplayScopeValidate 检查三个标识都必须非空。
func TestReplayScopeValidate(t *testing.T) {
	if err := replayTestScope().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	invalid := []ReplayScope{
		{},
		{Adapter: "a", Connection: "c", Model: " "},
		{Adapter: "a", Connection: " ", Model: "m"},
		{Adapter: " ", Connection: "c", Model: "m"},
	}

	for _, scope := range invalid {
		if scope.Validate() == nil {
			t.Errorf("accepted scope %+v", scope)
		}
	}
}

// TestReplayValidate 检查摘要格式与私有数据的结构。
func TestReplayValidate(t *testing.T) {
	if err := (*Replay)(nil).Validate(); err != nil {
		t.Fatalf("nil replay: %v", err)
	}

	valid := &Replay{
		Scope:   replayTestScope(),
		Binding: replayTestBinding(),
		Data:    json.RawMessage(`{"state":1}`),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	invalid := []*Replay{
		{
			Scope:   ReplayScope{},
			Binding: replayTestBinding(),
			Data:    json.RawMessage(`{}`),
		},
		{
			Scope:   replayTestScope(),
			Binding: "deadbeef",
			Data:    json.RawMessage(`{}`),
		},
		{
			Scope:   replayTestScope(),
			Binding: "sha256:" + strings.Repeat("A", 64),
			Data:    json.RawMessage(`{}`),
		},
		{
			Scope:   replayTestScope(),
			Binding: "sha256:" + strings.Repeat("z", 64),
			Data:    json.RawMessage(`{}`),
		},
		{
			Scope:   replayTestScope(),
			Binding: replayTestBinding(),
			Data:    nil,
		},
		{
			Scope:   replayTestScope(),
			Binding: replayTestBinding(),
			Data:    json.RawMessage(`null`),
		},
		{
			Scope:   replayTestScope(),
			Binding: replayTestBinding(),
			Data:    json.RawMessage(`{`),
		},
		{
			Scope:   replayTestScope(),
			Binding: replayTestBinding(),
			Data:    json.RawMessage(`{"a":1,"a":2}`),
		},
	}

	for index, replay := range invalid {
		if replay.Validate() == nil {
			t.Errorf("accepted invalid replay %d", index)
		}
	}
}

// TestReplayValidateFor 检查作用域与摘要的两级分类。
func TestReplayValidateFor(t *testing.T) {
	binding := replayTestBinding()
	replay := &Replay{
		Scope:   replayTestScope(),
		Binding: binding,
		Data:    json.RawMessage(`{"state":1}`),
	}

	if err := replay.ValidateFor(replayTestScope(), binding); err != nil {
		t.Fatalf("ValidateFor: %v", err)
	}

	if (*Replay)(nil).ValidateFor(replayTestScope(), binding) == nil {
		t.Fatal("accepted nil replay")
	}

	scopeMismatch := replay.ValidateFor(
		ReplayScope{Adapter: "other", Connection: "c", Model: "m"},
		binding,
	)

	var classified *Error
	if !errors.As(scopeMismatch, &classified) || classified.Kind != ErrorUnsupported {
		t.Fatalf("scope mismatch = %v, want unsupported", scopeMismatch)
	}

	bindingMismatch := replay.ValidateFor(
		replayTestScope(),
		"sha256:"+strings.Repeat("b", 64),
	)
	if !errors.As(bindingMismatch, &classified) || classified.Kind != ErrorProtocol {
		t.Fatalf("binding mismatch = %v, want protocol", bindingMismatch)
	}

	if replay.ValidateFor(ReplayScope{}, binding) == nil {
		t.Fatal("accepted invalid target scope")
	}
	if replay.ValidateFor(replayTestScope(), "bogus") == nil {
		t.Fatal("accepted invalid target binding")
	}
}

// TestReplayCloneIsDeep 检查 Clone 不共享私有数据。
func TestReplayCloneIsDeep(t *testing.T) {
	replay := &Replay{
		Scope:   replayTestScope(),
		Binding: replayTestBinding(),
		Data:    json.RawMessage(`{"state":1}`),
	}

	clone := replay.Clone()
	clone.Data[0] = 'X'

	if replay.Data[0] != '{' {
		t.Fatal("Clone shared Data bytes")
	}
	if (*Replay)(nil).Clone() != nil {
		t.Fatal("Clone must preserve nil")
	}
}
