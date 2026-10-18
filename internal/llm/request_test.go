package llm

import (
	"encoding/json"
	"testing"
)

// requestTestText 构造一条只含文本的消息。
func requestTestText(role Role, text string) Message {
	return Message{Role: role, Parts: []Part{{Kind: PartText, Text: text}}}
}

// requestTestTool 构造一个参数 Schema 为对象的工具声明。
func requestTestTool(name string) ToolSchema {
	return ToolSchema{
		Name:       name,
		Parameters: json.RawMessage(`{"type":"object"}`),
	}
}

// requestTestCall 构造一条要求调用工具的 assistant 消息。
func requestTestCall(ids ...string) Message {
	parts := make([]Part, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, Part{
			Kind: PartToolCall,
			ToolCall: &ToolCall{
				ID:        id,
				Name:      "read_file",
				Arguments: "{}",
			},
		})
	}

	return Message{Role: RoleAssistant, Parts: parts}
}

// requestTestResult 构造一条关闭指定调用的反馈消息。
func requestTestResult(ids ...string) Message {
	parts := make([]Part, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, Part{
			Kind:     PartText,
			Text:     "done",
			ResultOf: &ToolResultRef{CallID: id},
		})
	}

	return Message{Role: RoleAssistant, Parts: parts}
}

// TestRequestValidateAccepts 检查合法的请求结构。
func TestRequestValidateAccepts(t *testing.T) {
	tests := []struct {
		name    string
		request Request
	}{
		{
			"minimal",
			Request{Model: "m", Messages: []Message{requestTestText(RoleUser, "hi")}},
		},
		{
			"system prefix",
			Request{Model: "m", Messages: []Message{
				requestTestText(RoleSystem, "be brief"),
				requestTestText(RoleUser, "hi"),
			}},
		},
		{
			"closed call",
			Request{Model: "m", Messages: []Message{
				requestTestText(RoleUser, "hi"),
				requestTestCall("call_1"),
				requestTestResult("call_1"),
			}},
		},
		{
			"two calls closed in one feedback message",
			Request{Model: "m", Messages: []Message{
				requestTestText(RoleUser, "hi"),
				requestTestCall("call_1", "call_2"),
				requestTestResult("call_1", "call_2"),
			}},
		},
		{
			"two calls closed in two feedback messages",
			Request{Model: "m", Messages: []Message{
				requestTestText(RoleUser, "hi"),
				requestTestCall("call_1", "call_2"),
				requestTestResult("call_1"),
				requestTestResult("call_2"),
			}},
		},
		{
			"tool choice named with declared tool",
			Request{
				Model:    "m",
				Messages: []Message{requestTestText(RoleUser, "hi")},
				Tools:    []ToolSchema{requestTestTool("read_file")},
				Config: &Config{ToolChoice: &ToolChoice{
					Mode: ToolChoiceNamed,
					Name: "read_file",
				}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.request.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

// TestRequestValidateRejects 检查历史闭合、工具声明与消息顺序规则。
func TestRequestValidateRejects(t *testing.T) {
	user := requestTestText(RoleUser, "hi")

	tests := []struct {
		name    string
		request *Request
	}{
		{"nil request", nil},
		{"missing model", &Request{Messages: []Message{user}}},
		{"missing messages", &Request{Model: "m"}},
		{
			"duplicate tool names",
			&Request{
				Model:    "m",
				Messages: []Message{user},
				Tools:    []ToolSchema{requestTestTool("read_file"), requestTestTool("read_file")},
			},
		},
		{
			"tool name with whitespace",
			&Request{
				Model:    "m",
				Messages: []Message{user},
				Tools:    []ToolSchema{requestTestTool(" read_file ")},
			},
		},
		{
			"required tool choice without tools",
			&Request{
				Model:    "m",
				Messages: []Message{user},
				Config:   &Config{ToolChoice: &ToolChoice{Mode: ToolChoiceRequired}},
			},
		},
		{
			"named tool choice with unknown tool",
			&Request{
				Model:    "m",
				Messages: []Message{user},
				Tools:    []ToolSchema{requestTestTool("read_file")},
				Config: &Config{ToolChoice: &ToolChoice{
					Mode: ToolChoiceNamed,
					Name: "missing",
				}},
			},
		},
		{
			"system message after user message",
			&Request{Model: "m", Messages: []Message{
				user,
				requestTestText(RoleSystem, "late"),
			}},
		},
		{
			"result for unknown call",
			&Request{Model: "m", Messages: []Message{
				user,
				requestTestResult("call_1"),
			}},
		},
		{
			"duplicate result for one call",
			&Request{Model: "m", Messages: []Message{
				user,
				requestTestCall("call_1"),
				requestTestResult("call_1"),
				requestTestResult("call_1"),
			}},
		},
		{
			"ordinary message before the call is closed",
			&Request{Model: "m", Messages: []Message{
				user,
				requestTestCall("call_1"),
				requestTestText(RoleUser, "again"),
			}},
		},
		{
			"trailing open call",
			&Request{Model: "m", Messages: []Message{
				user,
				requestTestCall("call_1"),
			}},
		},
		{
			"result for a call declared later",
			&Request{Model: "m", Messages: []Message{
				user,
				requestTestResult("call_1"),
				requestTestCall("call_1"),
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.request.Validate() == nil {
				t.Fatal("accepted invalid request")
			}
		})
	}
}

// TestRequestCloneIsDeep 检查 Clone 不共享消息、工具参数与配置。
func TestRequestCloneIsDeep(t *testing.T) {
	request := Request{
		Model: "m",
		Messages: []Message{
			requestTestText(RoleUser, "hi"),
		},
		Tools:  []ToolSchema{requestTestTool("read_file")},
		Config: &Config{Stop: []string{"END"}},
	}

	clone := request.Clone()

	clone.Messages[0].Parts[0].Text = "changed"
	clone.Tools[0].Parameters[0] = 'X'
	clone.Config.Stop[0] = "STOP"

	if request.Messages[0].Parts[0].Text != "hi" {
		t.Fatal("Clone shared message content")
	}
	if string(request.Tools[0].Parameters) != `{"type":"object"}` {
		t.Fatal("Clone shared tool parameters")
	}
	if request.Config.Stop[0] != "END" {
		t.Fatal("Clone shared config")
	}

	empty := &Request{Model: "m"}
	if empty.Clone().Messages != nil {
		t.Fatal("Clone turned nil Messages into a non-nil slice")
	}
	if (*Request)(nil).Clone() != nil {
		t.Fatal("Clone must preserve nil")
	}
}
