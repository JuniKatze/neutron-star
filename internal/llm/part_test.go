package llm

import (
	"encoding/json"
	"testing"
)

// partTestData 返回一个合法的内联来源，供媒体类内容块使用。
func partTestData() Source {
	return Source{Type: SourceData, Data: []byte{1, 2, 3}}
}

// partTestValidParts 列出每种 Kind 的合法内容块。
func partTestValidParts() []struct {
	name string
	part Part
} {
	return []struct {
		name string
		part Part
	}{
		{"text", Part{Kind: PartText, Text: "hello"}},
		{"reasoning", Part{Kind: PartReasoning, Text: "thinking"}},
		{"refusal", Part{Kind: PartRefusal, Text: "no"}},
		{"json", Part{Kind: PartJSON, JSON: json.RawMessage(`{"a":1}`)}},
		{"json null", Part{Kind: PartJSON, JSON: json.RawMessage(`null`)}},
		{
			"tool call",
			Part{Kind: PartToolCall, ToolCall: &ToolCall{
				ID:        "call_1",
				Name:      "read_file",
				Arguments: `{"path":"a"}`,
			}},
		},
		{
			"image",
			Part{Kind: PartImage, Image: &Image{
				Source:   partTestData(),
				MIMEType: "image/png",
			}},
		},
		{
			"audio",
			Part{Kind: PartAudio, Audio: &Audio{
				Source:   partTestData(),
				MIMEType: "audio/wav",
			}},
		},
		{
			"video",
			Part{Kind: PartVideo, Video: &Video{
				Source:   partTestData(),
				MIMEType: "video/mp4",
			}},
		},
		{
			"document",
			Part{Kind: PartDocument, Document: &Document{
				Source:   partTestData(),
				MIMEType: "application/pdf",
			}},
		},
		{
			"file",
			Part{Kind: PartFile, File: &File{
				Source: partTestData(),
			}},
		},
		{
			"tool error",
			Part{
				Kind:     PartError,
				Error:    &ToolError{Code: "boom", Message: "failed"},
				ResultOf: &ToolResultRef{CallID: "call_1"},
			},
		},
		{
			"empty tool result",
			Part{Kind: PartEmpty, ResultOf: &ToolResultRef{CallID: "call_1"}},
		},
		{
			"text tool result",
			Part{
				Kind:     PartText,
				Text:     "done",
				ResultOf: &ToolResultRef{CallID: "call_1"},
			},
		},
	}
}

// TestPartValidateAcceptsEveryKind 检查每种 Kind 的合法载荷。
func TestPartValidateAcceptsEveryKind(t *testing.T) {
	for _, test := range partTestValidParts() {
		t.Run(test.name, func(t *testing.T) {
			if err := test.part.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

// TestPartValidateRejectsInvalidParts 检查载荷缺失、载荷错配与关联规则。
func TestPartValidateRejectsInvalidParts(t *testing.T) {
	call := &ToolCall{ID: "call_1", Name: "read_file", Arguments: "{}"}
	ref := &ToolResultRef{CallID: "call_1"}

	tests := []struct {
		name string
		part Part
	}{
		{"zero value", Part{}},
		{"unknown kind", Part{Kind: "bogus", Text: "x"}},
		{"empty text", Part{Kind: PartText}},
		{"empty reasoning", Part{Kind: PartReasoning}},
		{"empty refusal", Part{Kind: PartRefusal}},
		{"text with image payload", Part{Kind: PartText, Text: "x", Image: &Image{Source: partTestData(), MIMEType: "image/png"}}},
		{"json without payload", Part{Kind: PartJSON}},
		{"json with text payload", Part{Kind: PartJSON, JSON: json.RawMessage(`{}`), Text: "x"}},
		{"tool call without payload", Part{Kind: PartToolCall}},
		{"tool call without id", Part{Kind: PartToolCall, ToolCall: &ToolCall{Name: "read_file"}}},
		{"tool call without name", Part{Kind: PartToolCall, ToolCall: &ToolCall{ID: "call_1"}}},
		{"image without payload", Part{Kind: PartImage}},
		{"image with wrong mime", Part{Kind: PartImage, Image: &Image{Source: partTestData(), MIMEType: "audio/wav"}}},
		{"document without payload", Part{Kind: PartDocument}},
		{"error without result", Part{Kind: PartError, Error: &ToolError{Message: "x"}}},
		{"empty without result", Part{Kind: PartEmpty}},
		{"result on reasoning", Part{Kind: PartReasoning, Text: "x", ResultOf: ref}},
		{"result on tool call", Part{Kind: PartToolCall, ToolCall: call, ResultOf: ref}},
		{"result on opaque", Part{Kind: PartOpaque, ResultOf: ref}},
		{"opaque without replay", Part{Kind: PartOpaque}},
		{"blank result call id", Part{Kind: PartEmpty, ResultOf: &ToolResultRef{CallID: " "}}},
		{"blank error message", Part{Kind: PartError, Error: &ToolError{Message: " "}, ResultOf: ref}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.part.Validate(); err == nil {
				t.Fatal("accepted invalid part")
			}
		})
	}
}

// TestPartOpaqueRequiresMatchingReplay 检查只有 Replay 的内容块：摘要必须与实际内容一致。
func TestPartOpaqueRequiresMatchingReplay(t *testing.T) {
	part := Part{Kind: PartOpaque}

	binding, err := part.Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}

	part.Replay = &Replay{
		Scope:   ReplayScope{Adapter: "a", Connection: "c", Model: "m"},
		Binding: binding,
		Data:    json.RawMessage(`{"hidden":true}`),
	}
	if err := part.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	stale := part
	stale.Replay = &Replay{
		Scope:   part.Replay.Scope,
		Binding: binding,
		Data:    json.RawMessage(`{"hidden":true}`),
	}
	stale.Kind = PartText
	stale.Text = "changed"
	if err := stale.Validate(); err == nil {
		t.Fatal("accepted mismatched replay binding")
	}
}

// TestPartCloneIsDeep 检查 Clone 不共享载荷内存。
func TestPartCloneIsDeep(t *testing.T) {
	source := Part{
		Kind: PartImage,
		JSON: json.RawMessage(`{"a":1}`),
		Image: &Image{
			Source:   Source{Type: SourceData, Data: []byte{1, 2, 3}},
			MIMEType: "image/png",
		},
		ResultOf: &ToolResultRef{CallID: "call_1"},
	}

	clone := source.Clone()

	clone.JSON[0] = 'X'
	clone.Image.Source.Data[0] = 9
	clone.ResultOf.CallID = "call_2"
	clone.Image.MIMEType = "image/jpeg"

	if string(source.JSON) != `{"a":1}` {
		t.Fatalf("Clone shared JSON bytes: %s", source.JSON)
	}
	if source.Image.Source.Data[0] != 1 {
		t.Fatal("Clone shared source bytes")
	}
	if source.ResultOf.CallID != "call_1" {
		t.Fatal("Clone shared result reference")
	}
	if source.Image.MIMEType != "image/png" {
		t.Fatal("Clone shared media")
	}
}

// TestPartBindingIgnoresReplay 检查摘要只看公开内容。
func TestPartBindingIgnoresReplay(t *testing.T) {
	part := Part{Kind: PartText, Text: "hello"}

	before, err := part.Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}

	part.Replay = &Replay{
		Scope:   ReplayScope{Adapter: "a", Connection: "c", Model: "m"},
		Binding: before,
		Data:    json.RawMessage(`{"state":1}`),
	}

	after, err := part.Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if before != after {
		t.Fatal("Replay changed the binding")
	}
}

// TestToolCallValidateArgumentsObject 检查执行前的参数对象校验。
func TestToolCallValidateArgumentsObject(t *testing.T) {
	valid := &ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"path":"a"}`}
	if err := valid.ValidateArgumentsObject(); err != nil {
		t.Fatalf("ValidateArgumentsObject: %v", err)
	}

	invalid := []*ToolCall{
		nil,
		{ID: "call_1", Name: "read_file", Arguments: `[]`},
		{ID: "call_1", Name: "read_file", Arguments: `{`},
		{ID: "call_1", Name: "read_file", Arguments: `{"a":1,"a":2}`},
		{ID: "call_1", Name: "read_file", Arguments: ``},
	}
	for index, call := range invalid {
		if call.ValidateArgumentsObject() == nil {
			t.Errorf("accepted invalid call %d", index)
		}
	}
}
