package llm

import (
	"encoding/json"
	"testing"
)

// responseFormatTestBool 返回指向指定布尔值的指针。
func responseFormatTestBool(value bool) *bool {
	return &value
}

// TestResponseFormatValidate 检查三种模式的字段组合。
func TestResponseFormatValidate(t *testing.T) {
	valid := []struct {
		name   string
		format *ResponseFormat
	}{
		{"nil format", nil},
		{"text", &ResponseFormat{Type: ResponseFormatText}},
		{"json object", &ResponseFormat{Type: ResponseFormatJSONObject}},
		{
			"json schema",
			&ResponseFormat{
				Type:   ResponseFormatJSONSchema,
				Name:   "answer",
				Schema: json.RawMessage(`{"type":"object"}`),
			},
		},
		{
			"json schema strict",
			&ResponseFormat{
				Type:   ResponseFormatJSONSchema,
				Name:   "answer",
				Schema: json.RawMessage(`{"type":"object"}`),
				Strict: responseFormatTestBool(true),
			},
		},
		{
			"json schema with description",
			&ResponseFormat{
				Type:        ResponseFormatJSONSchema,
				Name:        "answer",
				Description: "the final answer",
				Schema:      json.RawMessage(`{"type":"object"}`),
			},
		},
	}

	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			if err := test.format.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}

	invalid := []struct {
		name   string
		format *ResponseFormat
	}{
		{"zero value", &ResponseFormat{}},
		{"unknown type", &ResponseFormat{Type: "bogus"}},
		{"text with name", &ResponseFormat{Type: ResponseFormatText, Name: "answer"}},
		{"text with description", &ResponseFormat{Type: ResponseFormatText, Description: "x"}},
		{"text with schema", &ResponseFormat{Type: ResponseFormatText, Schema: json.RawMessage(`{}`)}},
		{"text with strict", &ResponseFormat{Type: ResponseFormatText, Strict: responseFormatTestBool(false)}},
		{"json object with schema", &ResponseFormat{Type: ResponseFormatJSONObject, Schema: json.RawMessage(`{}`)}},
		{"json schema without name", &ResponseFormat{Type: ResponseFormatJSONSchema, Schema: json.RawMessage(`{}`)}},
		{
			"json schema with blank name",
			&ResponseFormat{Type: ResponseFormatJSONSchema, Name: " ", Schema: json.RawMessage(`{}`)},
		},
		{
			"json schema with padded name",
			&ResponseFormat{Type: ResponseFormatJSONSchema, Name: " answer ", Schema: json.RawMessage(`{}`)},
		},
		{"json schema without schema", &ResponseFormat{Type: ResponseFormatJSONSchema, Name: "answer"}},
		{
			"json schema with array schema",
			&ResponseFormat{Type: ResponseFormatJSONSchema, Name: "answer", Schema: json.RawMessage(`[]`)},
		},
		{
			"json schema with invalid schema",
			&ResponseFormat{Type: ResponseFormatJSONSchema, Name: "answer", Schema: json.RawMessage(`{`)},
		},
	}

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if test.format.Validate() == nil {
				t.Fatal("accepted invalid response format")
			}
		})
	}
}

// TestResponseFormatCloneIsDeep 检查 Clone 不共享 Schema 与 Strict。
func TestResponseFormatCloneIsDeep(t *testing.T) {
	format := &ResponseFormat{
		Type:   ResponseFormatJSONSchema,
		Name:   "answer",
		Schema: json.RawMessage(`{"type":"object"}`),
		Strict: responseFormatTestBool(true),
	}

	clone := format.Clone()

	clone.Schema[0] = 'X'
	*clone.Strict = false

	if string(format.Schema) != `{"type":"object"}` {
		t.Fatal("Clone shared schema bytes")
	}
	if !*format.Strict {
		t.Fatal("Clone shared strict pointer")
	}
	if (*ResponseFormat)(nil).Clone() != nil {
		t.Fatal("Clone must preserve nil")
	}
}
