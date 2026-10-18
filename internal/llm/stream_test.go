package llm

import "testing"

// streamTestTextResponse 返回只含文本的完整结果。
func streamTestTextResponse(text string) *Response {
	return &Response{
		Message: Message{
			Role:  RoleAssistant,
			Parts: []Part{{Kind: PartText, Text: text}},
		},
		StopReason: StopEndTurn,
	}
}

// TestStreamEventValidate 检查事件类型与载荷的互斥关系。
func TestStreamEventValidate(t *testing.T) {
	valid := []*StreamEvent{
		streamTestTextDelta(t, "hello"),
		{Kind: StreamFinal, Final: streamTestTextResponse("hello")},
	}

	for index, event := range valid {
		if err := event.Validate(); err != nil {
			t.Errorf("valid event %d rejected: %v", index, err)
		}
	}

	invalid := []*StreamEvent{
		nil,
		{},
		{Kind: StreamDelta},
		{Kind: StreamFinal},
		{
			Kind:  StreamDelta,
			Delta: &PartDelta{PartIndex: 0, Kind: PartText, Text: "x"},
			Final: streamTestTextResponse("x"),
		},
		{
			Kind:  StreamFinal,
			Delta: &PartDelta{PartIndex: 0, Kind: PartText, Text: "x"},
			Final: streamTestTextResponse("x"),
		},
		{Kind: "bogus"},
	}

	for index, event := range invalid {
		if event.Validate() == nil {
			t.Errorf("accepted invalid event %d", index)
		}
	}
}

// TestPartDeltaValidate 检查增量位置、类型与载荷。
func TestPartDeltaValidate(t *testing.T) {
	valid := []*PartDelta{
		{PartIndex: 0, Kind: PartText, Text: "x"},
		{PartIndex: 1, Kind: PartReasoning, Text: "think"},
		{PartIndex: 2, Kind: PartRefusal, Text: "no"},
		{PartIndex: 3, Kind: PartToolCall, ToolCall: &ToolCallDelta{ID: "call_1"}},
		{PartIndex: 3, Kind: PartToolCall, ToolCall: &ToolCallDelta{Name: "read_file"}},
		{PartIndex: 3, Kind: PartToolCall, ToolCall: &ToolCallDelta{ArgumentsDelta: "{"}},
	}

	for index, delta := range valid {
		if err := delta.Validate(); err != nil {
			t.Errorf("valid delta %d rejected: %v", index, err)
		}
	}

	invalid := []*PartDelta{
		nil,
		{PartIndex: -1, Kind: PartText, Text: "x"},
		{PartIndex: 0, Kind: PartText},
		{PartIndex: 0, Kind: PartText, Text: "x", ToolCall: &ToolCallDelta{ID: "call_1"}},
		{PartIndex: 0, Kind: PartToolCall, Text: "x", ToolCall: &ToolCallDelta{ID: "call_1"}},
		{PartIndex: 0, Kind: PartToolCall, ToolCall: &ToolCallDelta{}},
		{PartIndex: 0, Kind: PartImage, Text: "x"},
	}

	for index, delta := range invalid {
		if delta.Validate() == nil {
			t.Errorf("accepted invalid delta %d", index)
		}
	}
}

// TestToolCallDeltaValidate 检查增量至少包含一个字段。
func TestToolCallDeltaValidate(t *testing.T) {
	if (*ToolCallDelta)(nil).Validate() == nil {
		t.Fatal("accepted nil delta")
	}
	if (&ToolCallDelta{}).Validate() == nil {
		t.Fatal("accepted empty delta")
	}
	if (&ToolCallDelta{ID: " "}).Validate() == nil {
		t.Fatal("accepted blank id")
	}
	if (&ToolCallDelta{Name: " \n"}).Validate() == nil {
		t.Fatal("accepted blank name")
	}
	if err := (&ToolCallDelta{ID: "call_1"}).Validate(); err != nil {
		t.Fatalf("valid delta rejected: %v", err)
	}
	if err := (&ToolCallDelta{ArgumentsDelta: " "}).Validate(); err != nil {
		t.Fatalf("arguments delta rejected: %v", err)
	}
}

// TestStreamConstructorsClone 检查构造函数复制输入，之后修改输入不影响事件。
func TestStreamConstructorsClone(t *testing.T) {
	delta := PartDelta{
		PartIndex: 0,
		Kind:      PartToolCall,
		ToolCall:  &ToolCallDelta{ID: "call_1"},
	}

	event, err := NewDelta(delta)
	if err != nil {
		t.Fatalf("NewDelta: %v", err)
	}

	delta.ToolCall.ID = "changed"
	if event.Delta.ToolCall.ID != "call_1" {
		t.Fatal("NewDelta kept a reference to the input delta")
	}

	response := streamTestTextResponse("hello")

	final, err := NewFinal(response)
	if err != nil {
		t.Fatalf("NewFinal: %v", err)
	}

	response.Message.Parts[0].Text = "changed"
	if final.Final.Message.Parts[0].Text != "hello" {
		t.Fatal("NewFinal kept a reference to the input response")
	}
}

// TestStreamConstructorsRejectInvalid 检查构造函数沿用校验规则。
func TestStreamConstructorsRejectInvalid(t *testing.T) {
	if _, err := NewDelta(PartDelta{PartIndex: 0, Kind: PartText}); err == nil {
		t.Fatal("accepted an empty text delta")
	}
	if _, err := NewFinal(nil); err == nil {
		t.Fatal("accepted a nil response")
	}
	if _, err := NewFinal(streamTestTextResponse("x")); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
}

// TestStreamEventCloneIsDeep 检查 Clone 复制增量载荷。
func TestStreamEventCloneIsDeep(t *testing.T) {
	event := &StreamEvent{
		Kind:  StreamDelta,
		Delta: &PartDelta{PartIndex: 0, Kind: PartToolCall, ToolCall: &ToolCallDelta{ID: "call_1"}},
	}

	clone := event.Clone()
	clone.Delta.ToolCall.ID = "changed"

	if event.Delta.ToolCall.ID != "call_1" {
		t.Fatal("Clone shared the delta payload")
	}
	if (*StreamEvent)(nil).Clone() != nil {
		t.Fatal("Clone must preserve nil")
	}
	if (*PartDelta)(nil).Clone() != nil {
		t.Fatal("PartDelta Clone must preserve nil")
	}
}
