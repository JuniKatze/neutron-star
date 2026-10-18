package chatcompletions

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

const (
	// AdapterID 是本适配器的稳定标识，用于核对供应商文件引用的归属。
	AdapterID = "chat_completions"

	// MaxTokensField 与 MaxCompletionTokensField 是输出上限可选的 wire 字段名。
	// max_tokens 已被 OpenAI 标记为 deprecated，且与 o 系列模型不兼容；
	// max_completion_tokens 是现行字段，其上限包含推理输出。
	MaxTokensField           = "max_tokens"
	MaxCompletionTokensField = "max_completion_tokens"

	// defaultReasoningField 是未显式配置时的 assistant 推理字段名。
	defaultReasoningField = "reasoning_content"

	// defaultMaxSSEEventBytes 是单个 SSE 事件的字节上限。
	defaultMaxSSEEventBytes = 16 << 20

	// defaultMaxErrorBodyBytes 是读取错误响应体时的字节上限。
	defaultMaxErrorBodyBytes = 64 << 10
)

// HTTPDoer 是适配器需要的 HTTP 能力；*http.Client 满足它。
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Options 配置一个适配器实例。
// 它只在构造时读取一次，之后客户端不再引用它。
type Options struct {
	// Endpoint 是必需的绝对 HTTP(S) POST 地址，必须包含完整端点路径。
	// 适配器不自动追加 /chat/completions，也不接受用户信息或片段标识。
	Endpoint string

	// APIKey 为空时不发送 Authorization 头。
	APIKey string

	// HTTPClient 为空时使用默认客户端。
	// 适配器不设客户端总超时：流式响应可能长时间无数据，超时交给 ctx。
	HTTPClient HTTPDoer

	// Headers 是附加请求头，在默认头之后应用，因此可以覆盖它们
	// （例如自定义 Authorization）。
	Headers map[string]string

	// IncludeUsage 请求服务在流结束前返回 usage。
	// 并非所有兼容端点都支持 stream_options，因此默认关闭。
	IncludeUsage bool

	// ExtraBody 是原样并入请求体的扩展字段。
	// 它与适配器生成的字段冲突时报错，而不是静默覆盖。
	ExtraBody map[string]json.RawMessage

	// MaxTokensParam 选择输出上限的 wire 字段名，取值为 MaxTokensField
	// 或 MaxCompletionTokensField；空值默认为 MaxTokensField。
	MaxTokensParam string

	// SupportsAssistantReasoning 声明端点是否支持 assistant 推理字段。
	// 默认 false。
	//
	// false：发送时丢弃历史 assistant 消息里的推理内容，接收时忽略响应
	// 中的推理内容。该选项本身就是“此端点没有推理字段”的声明，
	// 因此这种降级不再另行记录。
	//
	// true：按 AssistantReasoningField 指定的字段收发推理内容；历史
	// assistant 消息没有推理内容时发送空字符串。
	SupportsAssistantReasoning bool

	// AssistantReasoningField 是 assistant 推理字段的 wire 名，收发同名，
	// 仅在 SupportsAssistantReasoning 为 true 时有效。
	// 空值默认为 "reasoning_content"；未启用该能力时必须留空。
	// 非空值必须是小写字母开头的 lower_snake_case，且不与标准消息字段重名。
	AssistantReasoningField string

	// Logger 记录内容降级等运行信息。
	// 为空时不记录任何日志；适配器不会因此改变行为。
	Logger *slog.Logger
}

// reservedMessageFields 是自定义推理字段不得占用的标准消息字段名。
var reservedMessageFields = map[string]struct{}{
	"role":          {},
	"content":       {},
	"tool_calls":    {},
	"tool_call_id":  {},
	"name":          {},
	"refusal":       {},
	"function_call": {},
	"audio":         {},
	"annotations":   {},
}

// validate 检查选项并在需要时补默认值。
// 选项错误是配置问题，在构造期返回普通错误，不通过流报告。
func (o *Options) validate() error {
	endpoint, err := url.Parse(o.Endpoint)
	if err != nil {
		return fmt.Errorf("endpoint: %w", err)
	}
	if !endpoint.IsAbs() || endpoint.Host == "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return errors.New(
			"endpoint: must be an absolute HTTP(S) URL",
		)
	}
	if endpoint.User != nil {
		return errors.New(
			"endpoint: must not contain user information",
		)
	}
	if endpoint.Fragment != "" {
		return errors.New("endpoint: must not contain a fragment")
	}

	switch o.MaxTokensParam {
	case "":
		o.MaxTokensParam = MaxTokensField

	case MaxTokensField, MaxCompletionTokensField:

	default:
		return fmt.Errorf(
			"maxTokensParam: unknown value %q",
			o.MaxTokensParam,
		)
	}

	switch {
	case o.SupportsAssistantReasoning:
		if o.AssistantReasoningField == "" {
			o.AssistantReasoningField = defaultReasoningField
		}
		if err := validateReasoningField(o.AssistantReasoningField); err != nil {
			return err
		}

	case o.AssistantReasoningField != "":
		return errors.New(
			"assistantReasoningField requires supportsAssistantReasoning",
		)
	}

	for key, value := range o.ExtraBody {
		if strings.TrimSpace(key) == "" {
			return errors.New("extraBody: field name must not be empty")
		}
		if !json.Valid(value) {
			return fmt.Errorf("extraBody[%q]: invalid JSON", key)
		}
	}

	for name := range o.Headers {
		if strings.TrimSpace(name) == "" {
			return errors.New("headers: name must not be empty")
		}
	}

	return nil
}

// validateReasoningField 检查自定义推理字段名。
// 名称必须是小写字母开头的 wire 字段名，且不与标准消息字段重名。
func validateReasoningField(field string) error {
	if !isLowerSnake(field) {
		return fmt.Errorf(
			"assistantReasoningField: %q must start with a lowercase "+
				"letter and contain only lowercase letters, digits and "+
				"underscores",
			field,
		)
	}
	if _, reserved := reservedMessageFields[field]; reserved {
		return fmt.Errorf(
			"assistantReasoningField: %q conflicts with a standard "+
				"message field",
			field,
		)
	}

	return nil
}

// isLowerSnake 报告 value 是否为非空的 lower_snake_case 标识。
func isLowerSnake(value string) bool {
	if value == "" {
		return false
	}

	for index, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char == '_':
		case char >= '0' && char <= '9':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}

	return true
}
