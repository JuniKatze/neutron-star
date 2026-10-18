package llm

import "testing"

// responseTestText 构造只含文本的完整结果。
func responseTestText(text string, stop StopReason) *Response {
	return &Response{
		Message: Message{
			Role:  RoleAssistant,
			Parts: []Part{{Kind: PartText, Text: text}},
		},
		StopReason: stop,
	}
}

// responseTestCall 构造带一个工具调用的完整结果。
func responseTestCall(callID string, stop StopReason) *Response {
	return &Response{
		Message: Message{
			Role: RoleAssistant,
			Parts: []Part{{
				Kind: PartToolCall,
				ToolCall: &ToolCall{
					ID:        callID,
					Name:      "read_file",
					Arguments: "{}",
				},
			}},
		},
		StopReason: stop,
	}
}

// responseTestNumber 返回指向指定整数的指针。
func responseTestNumber(value int64) *int64 {
	return &value
}

// TestResponseValidateAccepts 检查模型输出与停止原因的组合。
func TestResponseValidateAccepts(t *testing.T) {
	tests := []struct {
		name     string
		response *Response
	}{
		{"text end turn", responseTestText("hello", StopEndTurn)},
		{"empty assistant", &Response{Message: Message{Role: RoleAssistant}, StopReason: StopEndTurn}},
		{"tool call", responseTestCall("call_1", StopToolUse)},
		{
			"truncated tool call",
			responseTestCall("call_1", StopMaxTokens),
		},
		{
			"refusal",
			&Response{
				Message: Message{
					Role:  RoleAssistant,
					Parts: []Part{{Kind: PartRefusal, Text: "no"}},
				},
				StopReason: StopRefusal,
			},
		},
		{
			"other stop reason with provider text",
			&Response{
				Message:            Message{Role: RoleAssistant, Parts: []Part{{Kind: PartText, Text: "x"}}},
				StopReason:         StopOther,
				ProviderStopReason: "vendor_specific",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.response.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

// TestResponseValidateRejects 检查角色、输出内容与停止原因的约束。
func TestResponseValidateRejects(t *testing.T) {
	ref := &ToolResultRef{CallID: "call_1"}

	tests := []struct {
		name     string
		response *Response
	}{
		{"nil response", nil},
		{
			"user role",
			&Response{
				Message:    Message{Role: RoleUser, Parts: []Part{{Kind: PartText, Text: "x"}}},
				StopReason: StopEndTurn,
			},
		},
		{
			"tool feedback in model output",
			&Response{
				Message: Message{Role: RoleAssistant, Parts: []Part{{
					Kind:     PartText,
					Text:     "x",
					ResultOf: ref,
				}}},
				StopReason: StopEndTurn,
			},
		},
		{
			// PartError 与 PartEmpty 必须是工具反馈（带 ResultOf），
			// 而模型输出不允许反馈，所以它们在生成结果里必然非法。
			"tool error in model output",
			&Response{
				Message: Message{Role: RoleAssistant, Parts: []Part{{
					Kind:  PartError,
					Error: &ToolError{Message: "x"},
				}}},
				StopReason: StopEndTurn,
			},
		},
		{
			"unknown stop reason",
			responseTestText("x", StopReason("bogus")),
		},
		{
			"other stop reason without provider text",
			&Response{
				Message:    Message{Role: RoleAssistant, Parts: []Part{{Kind: PartText, Text: "x"}}},
				StopReason: StopOther,
			},
		},
		{
			"tool use without calls",
			responseTestText("x", StopToolUse),
		},
		{
			"calls with end turn",
			responseTestCall("call_1", StopEndTurn),
		},
		{
			"calls with stop sequence",
			responseTestCall("call_1", StopSequence),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.response.Validate() == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

// TestUsageValidate 检查 token 统计的字段关系。
func TestUsageValidate(t *testing.T) {
	valid := []struct {
		name  string
		usage *Usage
	}{
		{"nil usage", nil},
		{"zero usage", &Usage{}},
		{"counts only", &Usage{
			InputTokens:  responseTestNumber(10),
			OutputTokens: responseTestNumber(4),
		}},
		{"cache and reasoning subsets", &Usage{
			InputTokens:     responseTestNumber(10),
			OutputTokens:    responseTestNumber(4),
			CacheReadTokens: responseTestNumber(6),
			ReasoningTokens: responseTestNumber(2),
		}},
		{
			// total 的口径各供应商不同，本包不再要求它与输入加输出相等。
			"total differs from input plus output",
			&Usage{
				InputTokens:  responseTestNumber(10),
				OutputTokens: responseTestNumber(4),
				TotalTokens:  responseTestNumber(99),
			},
		},
		{
			// total 只要求非负，不参与与输入、输出的大小比较。
			"input above total",
			&Usage{
				InputTokens: responseTestNumber(9),
				TotalTokens: responseTestNumber(1),
			},
		},
	}

	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			if err := test.usage.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}

	invalid := []struct {
		name  string
		usage *Usage
	}{
		{"negative input", &Usage{InputTokens: responseTestNumber(-1)}},
		{"negative total", &Usage{TotalTokens: responseTestNumber(-1)}},
		{
			"cache above input",
			&Usage{
				InputTokens:     responseTestNumber(1),
				CacheReadTokens: responseTestNumber(2),
			},
		},
		{
			"reasoning above output",
			&Usage{
				OutputTokens:    responseTestNumber(1),
				ReasoningTokens: responseTestNumber(2),
			},
		},
	}

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if test.usage.Validate() == nil {
				t.Fatal("accepted invalid usage")
			}
		})
	}
}

// TestResponseValidateFor 检查结果与请求历史的一致性。
func TestResponseValidateFor(t *testing.T) {
	request := &Request{Model: "m", Messages: []Message{
		requestTestText(RoleUser, "hi"),
		requestTestCall("call_1"),
		requestTestResult("call_1"),
	}}

	if err := responseTestText("ok", StopEndTurn).ValidateFor(request); err != nil {
		t.Fatalf("ValidateFor: %v", err)
	}

	conflict := responseTestCall("call_1", StopToolUse)
	if err := conflict.ValidateFor(request); err == nil {
		t.Fatal("accepted a call id reused from history")
	}

	if err := responseTestText("ok", StopEndTurn).ValidateFor(&Request{}); err == nil {
		t.Fatal("accepted an invalid request")
	}
}

// TestResponseCloneIsDeep 检查 Clone 不共享消息与统计。
func TestResponseCloneIsDeep(t *testing.T) {
	response := responseTestText("hello", StopEndTurn)
	response.Usage = &Usage{InputTokens: responseTestNumber(10)}

	clone := response.Clone()

	clone.Message.Parts[0].Text = "changed"
	*clone.Usage.InputTokens = 99

	if response.Message.Parts[0].Text != "hello" {
		t.Fatal("Clone shared message content")
	}
	if *response.Usage.InputTokens != 10 {
		t.Fatal("Clone shared usage counters")
	}
	if (*Response)(nil).Clone() != nil {
		t.Fatal("Clone must preserve nil")
	}
}
