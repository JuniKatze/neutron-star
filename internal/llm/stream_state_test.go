package llm

import (
	"context"
	"testing"
)

// TestGuardStreamRefusalConsistency 检查 refusal 增量与 final 不一致时必须失败。
//
// 回归用例：状态机的 applyDelta 与 verifyFinal 曾经漏了 PartRefusal 分支，
// 于是 refusal 的预览与 final 不一致既不被累计、也不被比对，静默通过。
func TestGuardStreamRefusalConsistency(t *testing.T) {
	delta, err := NewDelta(PartDelta{
		PartIndex: 0,
		Kind:      PartRefusal,
		Text:      "no",
	})
	if err != nil {
		t.Fatalf("delta rejected: %v", err)
	}

	raw := Stream(func(yield func(*StreamEvent, error) bool) {
		if !yield(delta, nil) {
			return
		}

		yield(&StreamEvent{
			Kind: StreamFinal,
			Final: &Response{
				Message: Message{
					Role:  RoleAssistant,
					Parts: []Part{{Kind: PartRefusal, Text: "yes"}},
				},
				StopReason: StopRefusal,
			},
		}, nil)
	})

	response, err := Consume(GuardStream(context.Background(), raw, nil), nil)
	if err == nil {
		t.Fatalf("mismatch not detected; response = %+v", response)
	}
}
