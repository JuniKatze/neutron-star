package llm

import (
	"encoding/json"
	"testing"
)

// toolTestBool 返回指向指定布尔值的指针。
func toolTestBool(value bool) *bool {
	return &value
}

// TestToolSchemaValidate 检查工具名称与参数 Schema 的结构校验。
func TestToolSchemaValidate(t *testing.T) {
	valid := []struct {
		name string
		tool ToolSchema
	}{
		{
			"minimal",
			ToolSchema{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
		{
			"with strict",
			ToolSchema{
				Name:        "read_file",
				Description: "read a file",
				Parameters:  json.RawMessage(`{"type":"object"}`),
				Strict:      toolTestBool(true),
			},
		},
		{
			"empty object schema",
			ToolSchema{Name: "ping", Parameters: json.RawMessage(`{}`)},
		},
	}

	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			if err := test.tool.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}

	invalid := []struct {
		name string
		tool *ToolSchema
	}{
		{"nil tool", nil},
		{"empty name", &ToolSchema{Parameters: json.RawMessage(`{}`)}},
		{"blank name", &ToolSchema{Name: " ", Parameters: json.RawMessage(`{}`)}},
		{"name with whitespace", &ToolSchema{Name: " read_file ", Parameters: json.RawMessage(`{}`)}},
		{"missing parameters", &ToolSchema{Name: "read_file"}},
		{"array parameters", &ToolSchema{Name: "read_file", Parameters: json.RawMessage(`[]`)}},
		{"scalar parameters", &ToolSchema{Name: "read_file", Parameters: json.RawMessage(`1`)}},
		{"invalid parameters", &ToolSchema{Name: "read_file", Parameters: json.RawMessage(`{`)}},
		{"duplicate keys in parameters", &ToolSchema{Name: "read_file", Parameters: json.RawMessage(`{"a":1,"a":2}`)}},
	}

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if test.tool.Validate() == nil {
				t.Fatal("accepted invalid tool schema")
			}
		})
	}
}

// TestToolSchemaCloneIsDeep 检查 Clone 不共享参数文档与 Strict 指针。
func TestToolSchemaCloneIsDeep(t *testing.T) {
	tool := ToolSchema{
		Name:       "read_file",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Strict:     toolTestBool(true),
	}

	clone := tool.Clone()

	clone.Parameters[0] = 'X'
	*clone.Strict = false

	if string(tool.Parameters) != `{"type":"object"}` {
		t.Fatal("Clone shared parameters")
	}
	if !*tool.Strict {
		t.Fatal("Clone shared strict pointer")
	}
}
