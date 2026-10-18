package chatcompletions

import (
	"encoding/json"
	"testing"
)

// optionsTestEndpoint 是一条合法端点，供选项测试复用。
const optionsTestEndpoint = "https://api.example.com/v1/chat/completions"

// optionsTestNew 用给定选项构造实例，并把端点补成合法值。
func optionsTestNew(t *testing.T, options Options) (*Client, error) {
	t.Helper()

	if options.Endpoint == "" {
		options.Endpoint = optionsTestEndpoint
	}

	return New(options)
}

func TestOptionsEndpointIsCheckedAtConstruction(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
	}{
		{"empty", ""},
		{"relative", "/v1/chat/completions"},
		{"no host", "https:///v1/chat/completions"},
		{"other scheme", "ftp://api.example.com/v1/chat/completions"},
		{"user information", "https://user:pass@api.example.com/v1"},
		{"fragment", optionsTestEndpoint + "#fragment"},
		{"bare word", "chat/completions"},
	}

	for _, testCase := range cases {
		_, err := New(Options{Endpoint: testCase.endpoint})
		if err == nil {
			t.Errorf("%s: expected an error", testCase.name)
		}
	}
}

func TestOptionsEndpointAcceptsFullPath(t *testing.T) {
	// 适配器不自己补 /chat/completions：它原样使用调用方给的完整地址。
	options := Options{Endpoint: "https://api.example.com/v1/chat/completions"}

	client, err := optionsTestNew(t, options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if client.endpoint.String() != options.Endpoint {
		t.Fatalf("endpoint: %q", client.endpoint)
	}
}

func TestOptionsMaxTokensParam(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"default", "", MaxTokensField},
		{MaxTokensField, MaxTokensField, MaxTokensField},
		{
			MaxCompletionTokensField,
			MaxCompletionTokensField,
			MaxCompletionTokensField,
		},
	}

	for _, testCase := range cases {
		client, err := optionsTestNew(
			t,
			Options{MaxTokensParam: testCase.value},
		)
		if err != nil {
			t.Fatalf("%s: New: %v", testCase.name, err)
		}
		if client.maxTokensParam != testCase.want {
			t.Fatalf(
				"%s: got %q, want %q",
				testCase.name,
				client.maxTokensParam,
				testCase.want,
			)
		}
	}

	if _, err := optionsTestNew(
		t,
		Options{MaxTokensParam: "max_output_tokens"},
	); err == nil {
		t.Fatal("unknown max tokens field must be rejected")
	}
}

func TestOptionsReasoningField(t *testing.T) {
	cases := []struct {
		name            string
		supports        bool
		field           string
		want            string
		constructFailed bool
	}{
		{
			name: "disabled",
			want: "",
		},
		{
			name:     "default field",
			supports: true,
			want:     defaultReasoningField,
		},
		{
			name:     "custom field",
			supports: true,
			field:    "reasoning_text",
			want:     "reasoning_text",
		},
		{
			name:            "field without capability",
			field:           "reasoning_text",
			constructFailed: true,
		},
		{
			name:            "uppercase",
			supports:        true,
			field:           "ReasoningText",
			constructFailed: true,
		},
		{
			name:            "leading digit",
			supports:        true,
			field:           "1reasoning",
			constructFailed: true,
		},
		{
			name:            "dash",
			supports:        true,
			field:           "reasoning-text",
			constructFailed: true,
		},
		{
			name:            "standard field",
			supports:        true,
			field:           "content",
			constructFailed: true,
		},
		{
			name:            "standard tool field",
			supports:        true,
			field:           "tool_calls",
			constructFailed: true,
		},
	}

	for _, testCase := range cases {
		client, err := optionsTestNew(t, Options{
			SupportsAssistantReasoning: testCase.supports,
			AssistantReasoningField:    testCase.field,
		})

		if testCase.constructFailed {
			if err == nil {
				t.Errorf("%s: expected an error", testCase.name)
			}
			continue
		}

		if err != nil {
			t.Fatalf("%s: New: %v", testCase.name, err)
		}
		if client.reasoningField != testCase.want {
			t.Fatalf(
				"%s: got %q, want %q",
				testCase.name,
				client.reasoningField,
				testCase.want,
			)
		}
	}
}

func TestOptionsExtraBodyIsChecked(t *testing.T) {
	cases := []struct {
		name  string
		value map[string]json.RawMessage
	}{
		{
			"empty name",
			map[string]json.RawMessage{"": json.RawMessage(`1`)},
		},
		{
			"blank name",
			map[string]json.RawMessage{"  ": json.RawMessage(`1`)},
		},
		{
			"invalid value",
			map[string]json.RawMessage{"seed": json.RawMessage(`{`)},
		},
	}

	for _, testCase := range cases {
		if _, err := optionsTestNew(
			t,
			Options{ExtraBody: testCase.value},
		); err == nil {
			t.Errorf("%s: expected an error", testCase.name)
		}
	}
}

func TestOptionsHeadersAreChecked(t *testing.T) {
	_, err := optionsTestNew(
		t,
		Options{Headers: map[string]string{"": "value"}},
	)
	if err == nil {
		t.Fatal("empty header name must be rejected")
	}
}

// 构造之后修改调用方持有的 map，不应改变已经建好的实例：
// 否则"构造期读一遍配置"这句话就不成立。
func TestOptionsAreCopiedAtConstruction(t *testing.T) {
	headers := map[string]string{"X-Test": "one"}
	extraBody := map[string]json.RawMessage{
		"seed": json.RawMessage(`1`),
	}

	client, err := optionsTestNew(t, Options{
		Headers:   headers,
		ExtraBody: extraBody,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	headers["X-Test"] = "two"
	headers["X-Added"] = "three"
	extraBody["seed"][0] = '2'
	extraBody["added"] = json.RawMessage(`3`)

	if client.headers["X-Test"] != "one" {
		t.Fatalf("headers changed: %v", client.headers)
	}
	if _, exists := client.headers["X-Added"]; exists {
		t.Fatalf("headers changed: %v", client.headers)
	}
	if string(client.extraBody["seed"]) != "1" {
		t.Fatalf("extra body changed: %v", client.extraBody)
	}
	if _, exists := client.extraBody["added"]; exists {
		t.Fatalf("extra body changed: %v", client.extraBody)
	}
}

func TestOptionsDefaultHTTPClient(t *testing.T) {
	client, err := optionsTestNew(t, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if client.httpClient == nil {
		t.Fatal("default http client is required")
	}
}
