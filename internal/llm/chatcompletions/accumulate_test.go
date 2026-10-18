package chatcompletions

import (
	"encoding/json"
	"errors"
	"testing"

	"neutron-star/internal/llm"
)

// accumulateTestChunk 把 JSON 载荷解码为一个流项。
func accumulateTestChunk(t *testing.T, payload string) *wireChunk {
	t.Helper()

	var chunk wireChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		t.Fatalf("decode chunk: %v (%s)", err, payload)
	}

	return &chunk
}

// accumulateTestAccept 接收一个载荷并返回产生的增量。
func accumulateTestAccept(
	t *testing.T,
	accumulator *accumulator,
	payload string,
) []*llm.PartDelta {
	t.Helper()

	deltas, err := accumulator.accept(accumulateTestChunk(t, payload))
	if err != nil {
		t.Fatalf("accept: %v (%s)", err, payload)
	}

	return deltas
}

// accumulateTestTextDelta 描述一个文本类增量。
func accumulateTestTextDelta(delta *llm.PartDelta) string {
	return string(delta.Kind) + "|" + delta.Text
}

func TestAccumulateAppendsTextIntoOnePart(t *testing.T) {
	accumulator := newAccumulator("")

	first := accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
	)
	second := accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
	)

	if len(first) != 1 || first[0].PartIndex != 0 ||
		accumulateTestTextDelta(first[0]) != "text|Hel" {
		t.Fatalf("first delta: %+v", first)
	}
	if len(second) != 1 || second[0].PartIndex != 0 ||
		accumulateTestTextDelta(second[0]) != "text|lo" {
		t.Fatalf("second delta: %+v", second)
	}

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)

	response, err := accumulator.response("req-1")
	if err != nil {
		t.Fatalf("response: %v", err)
	}

	parts := response.Message.Parts
	if len(parts) != 1 || parts[0].Kind != llm.PartText ||
		parts[0].Text != "Hello" {
		t.Fatalf("parts: %+v", parts)
	}
	if response.StopReason != llm.StopEndTurn {
		t.Fatalf("stop reason: %q", response.StopReason)
	}
	if response.ProviderStopReason != "stop" {
		t.Fatalf("provider stop reason: %q", response.ProviderStopReason)
	}
	if response.Meta.RequestID != "req-1" {
		t.Fatalf("request id: %q", response.Meta.RequestID)
	}
}

func TestAccumulatePositionsFollowFirstAppearance(t *testing.T) {
	accumulator := newAccumulator(defaultReasoningField)

	deltas := accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"content":"a",`+
			`"reasoning_content":"r","tool_calls":[{"index":0,`+
			`"id":"call_a","function":{"name":"f","arguments":"{}"}}]}}]}`,
	)

	// 同一项里的顺序是固定的：文本、拒绝、推理、工具调用。
	want := []string{
		"text|a",
		"reasoning|r",
		"tool_call|call_a|f|{}",
	}

	if len(deltas) != len(want) {
		t.Fatalf("deltas: %+v", deltas)
	}
	for index := range deltas {
		if chatTestDelta(deltas[index]) != want[index] {
			t.Fatalf(
				"delta %d: got %q, want %q",
				index,
				chatTestDelta(deltas[index]),
				want[index],
			)
		}
		if deltas[index].PartIndex != index {
			t.Fatalf(
				"delta %d: part index %d",
				index,
				deltas[index].PartIndex,
			)
		}
	}

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	response, err := accumulator.response("")
	if err != nil {
		t.Fatalf("response: %v", err)
	}

	if len(response.Message.Parts) != 3 {
		t.Fatalf("parts: %+v", response.Message.Parts)
	}
	if response.Message.Parts[0].Kind != llm.PartText ||
		response.Message.Parts[1].Kind != llm.PartReasoning ||
		response.Message.Parts[2].Kind != llm.PartToolCall {
		t.Fatalf("parts: %+v", response.Message.Parts)
	}
}

func TestAccumulateReasoningRequiresField(t *testing.T) {
	payload := `{"choices":[{"index":0,` +
		`"delta":{"reasoning_content":"hmm"}}]}`

	enabled := newAccumulator(defaultReasoningField)

	deltas := accumulateTestAccept(t, enabled, payload)
	if len(deltas) != 1 ||
		accumulateTestTextDelta(deltas[0]) != "reasoning|hmm" {
		t.Fatalf("deltas: %+v", deltas)
	}

	// 声明端点不支持推理字段时，收到的推理内容不装配。
	disabled := newAccumulator("")

	if deltas := accumulateTestAccept(t, disabled, payload); len(deltas) != 0 {
		t.Fatalf("deltas: %+v", deltas)
	}
}

func TestAccumulateRefusalBecomesItsOwnPart(t *testing.T) {
	accumulator := newAccumulator("")

	deltas := accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"content":"ok",`+
			`"refusal":"I cannot help"}}]}`,
	)

	if len(deltas) != 2 {
		t.Fatalf("deltas: %+v", deltas)
	}
	if accumulateTestTextDelta(deltas[0]) != "text|ok" ||
		accumulateTestTextDelta(deltas[1]) != "refusal|I cannot help" {
		t.Fatalf("deltas: %+v", deltas)
	}

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)

	response, err := accumulator.response("")
	if err != nil {
		t.Fatalf("response: %v", err)
	}

	if len(response.Message.Parts) != 2 ||
		response.Message.Parts[1].Kind != llm.PartRefusal {
		t.Fatalf("parts: %+v", response.Message.Parts)
	}
}

func TestAccumulateEmptyContentProducesNoDelta(t *testing.T) {
	accumulator := newAccumulator("")

	if deltas := accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"content":""}}]}`,
	); len(deltas) != 0 {
		t.Fatalf("deltas: %+v", deltas)
	}
}

func TestAccumulateToolCallArgumentsAreAppended(t *testing.T) {
	accumulator := newAccumulator("")

	first := accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,`+
			`"id":"call_a","type":"function","function":`+
			`{"name":"weather","arguments":"{\"ci"}}]}}]}`,
	)
	second := accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,`+
			`"function":{"arguments":"ty\":\"Paris\"}"}}]}}]}`,
	)

	if len(first) != 1 || first[0].ToolCall.ID != "call_a" ||
		first[0].ToolCall.Name != "weather" ||
		first[0].ToolCall.ArgumentsDelta != `{"ci` {
		t.Fatalf("first delta: %+v", first[0].ToolCall)
	}
	if len(second) != 1 || second[0].ToolCall.ID != "" ||
		second[0].ToolCall.Name != "" ||
		second[0].ToolCall.ArgumentsDelta != `ty":"Paris"}` {
		t.Fatalf("second delta: %+v", second[0].ToolCall)
	}
	if second[0].PartIndex != first[0].PartIndex {
		t.Fatalf(
			"part index: %d then %d",
			first[0].PartIndex,
			second[0].PartIndex,
		)
	}

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	response, err := accumulator.response("")
	if err != nil {
		t.Fatalf("response: %v", err)
	}

	call := response.Message.Parts[0].ToolCall
	if call.ID != "call_a" || call.Name != "weather" ||
		call.Arguments != `{"city":"Paris"}` {
		t.Fatalf("tool call: %+v", call)
	}
}

func TestAccumulateToolCallWithoutPayloadIsIgnored(t *testing.T) {
	accumulator := newAccumulator("")

	// 只有序号、没有任何载荷的增量不分配位置。
	if deltas := accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0}]}}]}`,
	); len(deltas) != 0 {
		t.Fatalf("deltas: %+v", deltas)
	}

	deltas := accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"content":"a"}}]}`,
	)
	if deltas[0].PartIndex != 0 {
		t.Fatalf("part index: %d", deltas[0].PartIndex)
	}
}

func TestAccumulateToolCallConflicts(t *testing.T) {
	cases := []struct {
		name string
		want llm.ErrorKind
		next string
	}{
		{
			"conflicting id",
			"",
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,` +
				`"id":"call_b"}]}}]}`,
		},
		{
			"conflicting name",
			"",
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,` +
				`"function":{"name":"other"}}]}}]}`,
		},
		{
			"other call type",
			llm.ErrorUnsupported,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,` +
				`"type":"web_search"}]}}]}`,
		},
	}

	for _, testCase := range cases {
		accumulator := newAccumulator("")

		accumulateTestAccept(t, accumulator,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,`+
				`"id":"call_a","function":{"name":"f"}}]}}]}`,
		)

		_, err := accumulator.accept(accumulateTestChunk(t, testCase.next))
		if err == nil {
			t.Errorf("%s: expected an error", testCase.name)
			continue
		}

		if testCase.want == "" {
			continue
		}

		var failure *llm.Error
		if !errors.As(err, &failure) || failure.Kind != testCase.want {
			t.Errorf("%s: error %v", testCase.name, err)
		}
	}
}

func TestAccumulateRejectsRepeatedFinishReason(t *testing.T) {
	accumulator := newAccumulator("")

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)

	_, err := accumulator.accept(accumulateTestChunk(t,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	))
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestAccumulateRejectsOtherChoiceIndex(t *testing.T) {
	accumulator := newAccumulator("")

	_, err := accumulator.accept(accumulateTestChunk(t,
		`{"choices":[{"index":1,"delta":{"content":"a"},`+
			`"finish_reason":"stop"}]}`,
	))

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorUnsupported {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestAccumulateMetaAndUsage(t *testing.T) {
	accumulator := newAccumulator("")

	accumulateTestAccept(t, accumulator,
		`{"id":"chatcmpl-7","model":"test-model",`+
			`"choices":[{"index":0,"delta":{},"finish_reason":"length"}],`+
			`"usage":{"prompt_tokens":7,"completion_tokens":3,`+
			`"total_tokens":10,"prompt_tokens_details":{"cached_tokens":2},`+
			`"completion_tokens_details":{"reasoning_tokens":1}}}`,
	)

	response, err := accumulator.response("req-7")
	if err != nil {
		t.Fatalf("response: %v", err)
	}

	if response.Meta.ResponseID != "chatcmpl-7" ||
		response.Meta.Model != "test-model" ||
		response.Meta.RequestID != "req-7" {
		t.Fatalf("meta: %+v", response.Meta)
	}

	usage := response.Usage
	if usage == nil {
		t.Fatal("usage is missing")
	}
	if *usage.InputTokens != 7 || *usage.OutputTokens != 3 ||
		*usage.TotalTokens != 10 || *usage.CacheReadTokens != 2 ||
		*usage.ReasoningTokens != 1 {
		t.Fatalf("usage: %+v", usage)
	}
	if usage.CacheWriteTokens != nil {
		t.Fatalf("cache write: %+v", usage.CacheWriteTokens)
	}
	if response.StopReason != llm.StopMaxTokens {
		t.Fatalf("stop reason: %q", response.StopReason)
	}
}

func TestAccumulateUsageWithoutDetails(t *testing.T) {
	accumulator := newAccumulator("")

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],`+
			`"usage":{"total_tokens":5}}`,
	)

	response, err := accumulator.response("")
	if err != nil {
		t.Fatalf("response: %v", err)
	}

	if response.Usage == nil {
		t.Fatal("usage is missing")
	}
	if response.Usage.TotalTokens == nil ||
		*response.Usage.TotalTokens != 5 {
		t.Fatalf("usage: %+v", response.Usage)
	}
	if response.Usage.InputTokens != nil ||
		response.Usage.OutputTokens != nil ||
		response.Usage.CacheReadTokens != nil ||
		response.Usage.ReasoningTokens != nil {
		t.Fatalf("usage: %+v", response.Usage)
	}
}

func TestAccumulateUsageChunkWithoutChoices(t *testing.T) {
	accumulator := newAccumulator("")

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"content":"a"},`+
			`"finish_reason":"stop"}]}`,
	)

	// 统计项没有候选，也不产生增量。
	if deltas := accumulateTestAccept(t, accumulator,
		`{"choices":[],"usage":{"total_tokens":9}}`,
	); len(deltas) != 0 {
		t.Fatalf("deltas: %+v", deltas)
	}

	response, err := accumulator.response("")
	if err != nil {
		t.Fatalf("response: %v", err)
	}
	if response.Usage == nil || *response.Usage.TotalTokens != 9 {
		t.Fatalf("usage: %+v", response.Usage)
	}
}

func TestAccumulateRequiresFinishReason(t *testing.T) {
	accumulator := newAccumulator("")

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"content":"a"}}]}`,
	)

	_, err := accumulator.response("")

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorProtocol {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestAccumulateNormalizesStopReason(t *testing.T) {
	cases := []struct {
		name       string
		finish     string
		hasCalls   bool
		want       llm.StopReason
		expectFail bool
	}{
		{"natural stop", "stop", false, llm.StopEndTurn, false},
		{"stop with calls", "stop", true, llm.StopToolUse, false},
		{"tool calls", "tool_calls", true, llm.StopToolUse, false},
		{"legacy function call", "function_call", true, llm.StopToolUse, false},
		{"length", "length", false, llm.StopMaxTokens, false},
		{
			"content filter",
			"content_filter",
			false,
			llm.StopRefusal,
			false,
		},
		{"unknown", "provider_specific", false, llm.StopOther, false},
		{"calls without calls", "tool_calls", false, "", true},
		{"empty", "", false, "", true},
	}

	for _, testCase := range cases {
		got, err := stopReason(testCase.finish, testCase.hasCalls)

		if testCase.expectFail {
			if err == nil {
				t.Errorf("%s: expected an error", testCase.name)
			}
			continue
		}

		if err != nil {
			t.Errorf("%s: %v", testCase.name, err)
			continue
		}
		if got != testCase.want {
			t.Errorf(
				"%s: got %q, want %q",
				testCase.name,
				got,
				testCase.want,
			)
		}
	}
}

func TestAccumulateUnknownStopReasonKeepsProviderValue(t *testing.T) {
	accumulator := newAccumulator("")

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"content":"a"},`+
			`"finish_reason":"provider_specific"}]}`,
	)

	response, err := accumulator.response("")
	if err != nil {
		t.Fatalf("response: %v", err)
	}

	if response.StopReason != llm.StopOther {
		t.Fatalf("stop reason: %q", response.StopReason)
	}
	if response.ProviderStopReason != "provider_specific" {
		t.Fatalf(
			"provider stop reason: %q",
			response.ProviderStopReason,
		)
	}
}

func TestAccumulateStopWithCallsBecomesToolUse(t *testing.T) {
	accumulator := newAccumulator("")

	accumulateTestAccept(t, accumulator,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,`+
			`"id":"call_a","function":{"name":"f","arguments":"{}"}}]},`+
			`"finish_reason":"stop"}]}`,
	)

	response, err := accumulator.response("")
	if err != nil {
		t.Fatalf("response: %v", err)
	}

	if response.StopReason != llm.StopToolUse {
		t.Fatalf("stop reason: %q", response.StopReason)
	}
	if response.ProviderStopReason != "stop" {
		t.Fatalf(
			"provider stop reason: %q",
			response.ProviderStopReason,
		)
	}
}

func TestAccumulateClassifyStreamError(t *testing.T) {
	classified := protocolError("known", nil)

	if got := classifyStreamError(classified); got != classified {
		t.Fatalf("classified error was wrapped: %v", got)
	}

	got := classifyStreamError(errors.New("raw"))

	var failure *llm.Error
	if !errors.As(got, &failure) {
		t.Fatalf("error type: %v", got)
	}
	if failure.Kind != llm.ErrorProtocol {
		t.Fatalf("kind: %q", failure.Kind)
	}
}
