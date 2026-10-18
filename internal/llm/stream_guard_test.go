package llm

import (
	"context"
	"errors"
	"testing"
)

// streamTestTextDelta 构造一个第 0 位置、文本类型的增量事件。
func streamTestTextDelta(t *testing.T, text string) *StreamEvent {
	t.Helper()

	event, err := NewDelta(PartDelta{
		PartIndex: 0,
		Kind:      PartText,
		Text:      text,
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}

// streamTestTextFinal 构造一个只含指定文本的完整结果事件。
func streamTestTextFinal(t *testing.T, text string) *StreamEvent {
	t.Helper()

	event, err := NewFinal(&Response{
		Message: Message{
			Role:  RoleAssistant,
			Parts: []Part{{Kind: PartText, Text: text}},
		},
		StopReason: StopEndTurn,
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}

// TestConsumeDeliversResponse 检查正常流：delta 交给 preview，final 作为结果返回。
func TestConsumeDeliversResponse(t *testing.T) {
	raw := Stream(func(yield func(*StreamEvent, error) bool) {
		if !yield(streamTestTextDelta(t, "hello"), nil) {
			return
		}

		yield(streamTestTextFinal(t, "hello"), nil)
	})

	previews := 0

	response, err := Consume(
		GuardStream(context.Background(), raw, nil),
		func(*PartDelta) error {
			previews++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if previews != 1 {
		t.Fatalf("previews = %d, want 1", previews)
	}
	if response.Message.Parts[0].Text != "hello" {
		t.Fatalf("response = %+v", response)
	}
}

// TestConsumePreviewFailure 检查 preview 报错时不返回结果，并原样返回该错误。
func TestConsumePreviewFailure(t *testing.T) {
	boom := errors.New("boom")

	raw := Stream(func(yield func(*StreamEvent, error) bool) {
		yield(streamTestTextDelta(t, "x"), nil)
		yield(streamTestTextFinal(t, "x"), nil)
	})

	response, err := Consume(
		GuardStream(context.Background(), raw, nil),
		func(*PartDelta) error { return boom },
	)

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if response != nil {
		t.Fatalf("response = %+v, want nil", response)
	}
}

// TestGuardStreamEarlyExitCleanupOnce 检查下游提前退出时停止上游，且 cleanup 恰好一次。
func TestGuardStreamEarlyExitCleanupOnce(t *testing.T) {
	cleanups := 0
	seen := 0

	raw := Stream(func(yield func(*StreamEvent, error) bool) {
		if !yield(streamTestTextDelta(t, "a"), nil) {
			return
		}

		yield(streamTestTextDelta(t, "b"), nil)
	})

	for range GuardStream(context.Background(), raw, func() {
		cleanups++
	}) {
		seen++
		break
	}

	if seen != 1 {
		t.Fatalf("seen = %d, want 1", seen)
	}
	if cleanups != 1 {
		t.Fatalf("cleanups = %d, want 1", cleanups)
	}
}

// TestGuardStreamCancelBetweenItems 检查消费过程中取消被归类为取消，而不是协议错误。
func TestGuardStreamCancelBetweenItems(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	raw := Stream(func(yield func(*StreamEvent, error) bool) {
		if !yield(streamTestTextDelta(t, "a"), nil) {
			return
		}

		cancel()
		yield(streamTestTextDelta(t, "b"), nil)
	})

	var failures []error
	for _, err := range GuardStream(ctx, raw, nil) {
		if err != nil {
			failures = append(failures, err)
		}
	}

	if len(failures) != 1 {
		t.Fatalf("failures = %v, want exactly one", failures)
	}

	classified, ok := failures[0].(*Error)
	if !ok || classified.Kind != ErrorCanceled {
		t.Fatalf("err = %v, want canceled", failures[0])
	}
}
