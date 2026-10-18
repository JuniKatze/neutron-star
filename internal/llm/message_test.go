package llm

import (
	"encoding/json"
	"testing"
)

// messageTestText 构造一条只含文本的消息。
func messageTestText(role Role, text string) Message {
	return Message{
		Role:  role,
		Parts: []Part{{Kind: PartText, Text: text}},
	}
}

// messageTestResult 构造一条把文本作为工具结果的反馈消息。
func messageTestResult(callID, text string) Message {
	return Message{
		Role: RoleAssistant,
		Parts: []Part{{
			Kind:     PartText,
			Text:     text,
			ResultOf: &ToolResultRef{CallID: callID},
		}},
	}
}

// messageTestScope 返回测试用的回放作用域。
func messageTestScope() ReplayScope {
	return ReplayScope{Adapter: "a", Connection: "c", Model: "m"}
}

// TestMessageValidateAccepts 检查合法消息。
func TestMessageValidateAccepts(t *testing.T) {
	tests := []struct {
		name    string
		message Message
	}{
		{"system text", messageTestText(RoleSystem, "be brief")},
		{"user text", messageTestText(RoleUser, "hi")},
		{"assistant empty", Message{Role: RoleAssistant}},
		{"assistant text", messageTestText(RoleAssistant, "hello")},
		{
			"assistant tool call",
			Message{Role: RoleAssistant, Parts: []Part{{
				Kind:     PartToolCall,
				ToolCall: &ToolCall{ID: "call_1", Name: "read_file", Arguments: "{}"},
			}}},
		},
		{"tool feedback text", messageTestResult("call_1", "done")},
		{
			"tool feedback empty",
			Message{Role: RoleAssistant, Parts: []Part{{
				Kind:     PartEmpty,
				ResultOf: &ToolResultRef{CallID: "call_1"},
			}}},
		},
		{
			"one call with two result parts",
			Message{Role: RoleAssistant, Parts: []Part{
				{Kind: PartText, Text: "a", ResultOf: &ToolResultRef{CallID: "call_1"}},
				{Kind: PartText, Text: "b", ResultOf: &ToolResultRef{CallID: "call_1"}},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.message.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

// TestMessageValidateRejects 检查角色、内容白名单与消息内关联规则。
func TestMessageValidateRejects(t *testing.T) {
	ref := &ToolResultRef{CallID: "call_1"}
	call := &ToolCall{ID: "call_1", Name: "read_file", Arguments: "{}"}

	tests := []struct {
		name    string
		message *Message
	}{
		{"nil message", nil},
		{"unknown role", &Message{Role: "bogus", Parts: []Part{{Kind: PartText, Text: "x"}}}},
		{"system without parts", &Message{Role: RoleSystem}},
		{"user without parts", &Message{Role: RoleUser}},
		{
			"system with json",
			&Message{Role: RoleSystem, Parts: []Part{{
				Kind: PartJSON,
				JSON: json.RawMessage(`{}`),
			}}},
		},
		{
			"user with tool call",
			&Message{Role: RoleUser, Parts: []Part{{Kind: PartToolCall, ToolCall: call}}},
		},
		{
			"user with reasoning",
			&Message{Role: RoleUser, Parts: []Part{{Kind: PartReasoning, Text: "x"}}},
		},
		{
			"feedback in user message",
			&Message{Role: RoleUser, Parts: []Part{{
				Kind:     PartText,
				Text:     "x",
				ResultOf: ref,
			}}},
		},
		{
			"feedback mixed with ordinary content",
			&Message{Role: RoleAssistant, Parts: []Part{
				{Kind: PartText, Text: "x", ResultOf: ref},
				{Kind: PartText, Text: "y"},
			}},
		},
		{
			"duplicate call ids",
			&Message{Role: RoleAssistant, Parts: []Part{
				{Kind: PartToolCall, ToolCall: call},
				{Kind: PartToolCall, ToolCall: call},
			}},
		},
		{
			"empty result with another part in the group",
			&Message{Role: RoleAssistant, Parts: []Part{
				{Kind: PartEmpty, ResultOf: ref},
				{Kind: PartText, Text: "x", ResultOf: ref},
			}},
		},
		{
			"two empty results in the same group",
			&Message{Role: RoleAssistant, Parts: []Part{
				{Kind: PartEmpty, ResultOf: ref},
				{Kind: PartEmpty, ResultOf: ref},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.message.Validate() == nil {
				t.Fatal("accepted invalid message")
			}
		})
	}
}

// TestMessageBindingProperties 检查消息摘要只反映角色与有序内容。
func TestMessageBindingProperties(t *testing.T) {
	message := messageTestText(RoleAssistant, "hello")

	first, err := message.Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}

	message.Replay = &Replay{
		Scope:   messageTestScope(),
		Binding: first,
		Data:    json.RawMessage(`{"state":1}`),
	}
	if err := message.Validate(); err != nil {
		t.Fatalf("Validate with replay: %v", err)
	}

	withReplay, err := message.Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if first != withReplay {
		t.Fatal("Replay changed the message binding")
	}

	changedRole := message
	changedRole.Role = RoleUser
	roleBinding, err := changedRole.Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if roleBinding == first {
		t.Fatal("binding omitted role")
	}

	ordered := Message{Role: RoleAssistant, Parts: []Part{
		{Kind: PartText, Text: "first"},
		{Kind: PartText, Text: "second"},
	}}
	before, err := ordered.Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	ordered.Parts[0], ordered.Parts[1] = ordered.Parts[1], ordered.Parts[0]
	after, err := ordered.Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if before == after {
		t.Fatal("binding omitted part order")
	}

	nilParts, err := (&Message{Role: RoleAssistant}).Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	emptyParts, err := (&Message{Role: RoleAssistant, Parts: []Part{}}).Binding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if nilParts != emptyParts {
		t.Fatal("nil and empty Parts must have the same binding")
	}
}

// TestMessageCloneIsDeep 检查 Clone 不共享内容块与回放。
func TestMessageCloneIsDeep(t *testing.T) {
	message := Message{
		Role: RoleAssistant,
		Parts: []Part{
			{Kind: PartText, Text: "hello"},
			{
				Kind: PartImage,
				Image: &Image{
					Source:   Source{Type: SourceData, Data: []byte{1}},
					MIMEType: "image/png",
				},
			},
		},
	}

	clone := message.Clone()
	clone.Parts[0].Text = "changed"
	clone.Parts[1].Image.Source.Data[0] = 9

	if message.Parts[0].Text != "hello" {
		t.Fatal("Clone shared Part value")
	}
	if message.Parts[1].Image.Source.Data[0] != 1 {
		t.Fatal("Clone shared Part payload")
	}

	if empty := (&Message{Role: RoleAssistant}).Clone(); empty.Parts != nil {
		t.Fatal("Clone turned nil Parts into a non-nil slice")
	}
	if (&Message{}).Clone() == nil {
		t.Fatal("Clone returned nil for a non-nil message")
	}
	if (*Message)(nil).Clone() != nil {
		t.Fatal("Clone must preserve nil")
	}
}

// TestMessageIsToolResultMessage 检查反馈消息的判据。
func TestMessageIsToolResultMessage(t *testing.T) {
	feedback := messageTestResult("call_1", "done")
	if !feedback.IsToolResultMessage() {
		t.Fatal("feedback message not recognized")
	}

	ordinary := messageTestText(RoleAssistant, "hello")
	if ordinary.IsToolResultMessage() {
		t.Fatal("ordinary message recognized as feedback")
	}

	empty := &Message{Role: RoleAssistant}
	if empty.IsToolResultMessage() {
		t.Fatal("empty assistant message recognized as feedback")
	}
	if (*Message)(nil).IsToolResultMessage() {
		t.Fatal("nil message recognized as feedback")
	}
}
