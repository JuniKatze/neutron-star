package chatcompletions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"strings"

	"neutron-star/internal/llm"
)

// 本文件把已冻结的请求投影为 Chat Completions 请求体。
//
// 三类处理：
//
//   - 契约等价的规整：多个文本块拼接成一个 content、单一文本块降为字符串、
//     工具反馈按调用展开为多条 role:"tool" 消息。
//   - 内容降级（丢弃）：wire 上没有对应表达的内容块被跳过，请求照常发送；
//     启用 Logger 时记一条 warn。被丢弃之后为空的消息仍然发送（内容为空），
//     因为它在时间线里存在，删掉整条消息会改变对话结构。
//   - 参数不支持（报错）：IR 能表达、但本协议没有字段的生成选项返回
//     ErrorUnsupported——参数没有安全降级。
//
// assistant 推理内容按 SupportsAssistantReasoning 处理：该选项为 false 时
// 丢弃历史推理，这是配置声明的降级，不另行记录。

// buildBody 投影请求体。
func (c *Client) buildBody(request *llm.Request) ([]byte, error) {
	messages, err := c.projectMessages(request)
	if err != nil {
		return nil, err
	}

	body := requestBody{
		Model:    request.Model,
		Messages: messages,
		Stream:   true,
	}

	if c.includeUsage {
		body.StreamOptions = &wireStreamOptions{IncludeUsage: true}
	}

	if len(request.Tools) > 0 {
		body.Tools = make([]wireTool, len(request.Tools))

		for index := range request.Tools {
			tool := &request.Tools[index]

			body.Tools[index] = wireTool{
				Type: "function",
				Function: wireToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
					Strict:      tool.Strict,
				},
			}
		}
	}

	if config := request.Config; config != nil {
		if err := c.applyConfig(&body, config); err != nil {
			return nil, err
		}
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, invalidRequestError("encode request body", err)
	}

	return c.mergeExtraBody(encoded)
}

// applyConfig 把生成选项投影到请求体字段。
func (c *Client) applyConfig(
	body *requestBody,
	config *llm.Config,
) error {
	body.Temperature = config.Temperature
	body.TopP = config.TopP
	body.TopK = config.TopK
	body.Stop = config.Stop

	if config.MaxOutputTokens != nil {
		if c.maxTokensParam == MaxCompletionTokensField {
			body.MaxCompletionTokens = config.MaxOutputTokens
		} else {
			body.MaxTokens = config.MaxOutputTokens
		}
	}

	if choice := config.ToolChoice; choice != nil {
		encoded, err := projectToolChoice(choice)
		if err != nil {
			return err
		}

		body.ToolChoice = encoded
		body.ParallelToolCalls = choice.Parallel
	}

	if format := config.ResponseFormat; format != nil {
		body.ResponseFormat = projectResponseFormat(format)
	}

	if reasoning := config.Reasoning; reasoning != nil {
		effort, err := projectReasoningEffort(reasoning)
		if err != nil {
			return err
		}

		body.ReasoningEffort = effort
	}

	return nil
}

// projectToolChoice 投影工具选择策略。
// IR 已保证 Mode 合法且 Name 与模式相容。
func projectToolChoice(
	choice *llm.ToolChoice,
) (json.RawMessage, error) {
	switch choice.Mode {
	case llm.ToolChoiceAuto:
		return json.RawMessage(`"auto"`), nil

	case llm.ToolChoiceNone:
		return json.RawMessage(`"none"`), nil

	case llm.ToolChoiceRequired:
		return json.RawMessage(`"required"`), nil

	case llm.ToolChoiceNamed:
		encoded, err := json.Marshal(wireNamedToolChoice{
			Type:     "function",
			Function: wireToolCallName{Name: choice.Name},
		})
		if err != nil {
			return nil, invalidRequestError("encode tool choice", err)
		}

		return encoded, nil
	}

	return nil, unsupportedError(fmt.Sprintf(
		"config.toolChoice.mode %q has no chat completions equivalent",
		choice.Mode,
	))
}

// projectResponseFormat 投影输出格式约束。
// IR 已保证 Type 合法、字段组合正确、Schema 是 JSON 对象。
func projectResponseFormat(
	format *llm.ResponseFormat,
) *wireResponseFormat {
	switch format.Type {
	case llm.ResponseFormatText:
		return &wireResponseFormat{Type: "text"}

	case llm.ResponseFormatJSONObject:
		return &wireResponseFormat{Type: "json_object"}
	}

	return &wireResponseFormat{
		Type: "json_schema",
		JSONSchema: &wireJSONSchema{
			Name:        format.Name,
			Description: format.Description,
			Schema:      format.Schema,
			Strict:      format.Strict,
		},
	}
}

// projectReasoningEffort 投影推理选项。
//
// Chat Completions 只有一个归一化强度字段：预算与摘要没有对应字段，
// 超出该字段取值域的强度也不被接受。这些属于参数类不支持，必须报错。
func projectReasoningEffort(
	reasoning *llm.Reasoning,
) (string, error) {
	if reasoning.BudgetTokens != nil {
		return "", unsupportedError(
			"config.reasoning.budgetTokens has no chat completions equivalent",
		)
	}
	if reasoning.Summary != nil && *reasoning.Summary {
		return "", unsupportedError(
			"config.reasoning.summary has no chat completions equivalent",
		)
	}

	switch reasoning.Effort {
	case "":

	case llm.ReasoningNone,
		llm.ReasoningMinimal,
		llm.ReasoningLow,
		llm.ReasoningMedium,
		llm.ReasoningHigh:
		return string(reasoning.Effort), nil

	default:
		return "", unsupportedError(fmt.Sprintf(
			"config.reasoning.effort %q has no chat completions equivalent",
			reasoning.Effort,
		))
	}

	if reasoning.Enabled != nil {
		if *reasoning.Enabled {
			return "", unsupportedError(
				"config.reasoning.enabled needs an explicit effort",
			)
		}

		// 显式关闭推理：none 是当前唯一的表达方式。
		return string(llm.ReasoningNone), nil
	}

	return "", nil
}

// projectMessages 投影全部消息，并把工具反馈展开为多条 tool 消息。
func (c *Client) projectMessages(
	request *llm.Request,
) ([]json.RawMessage, error) {
	encoded := make([]json.RawMessage, 0, len(request.Messages))

	for index := range request.Messages {
		message := &request.Messages[index]
		location := fmt.Sprintf("messages[%d]", index)

		if message.IsToolResultMessage() {
			expanded, err := c.projectToolFeedback(message, location)
			if err != nil {
				return nil, err
			}

			encoded = append(encoded, expanded...)
			continue
		}

		single, err := c.projectMessage(message, location)
		if err != nil {
			return nil, err
		}

		encoded = append(encoded, single)
	}

	return encoded, nil
}

// projectMessage 投影一条非反馈消息。
func (c *Client) projectMessage(
	message *llm.Message,
	location string,
) (json.RawMessage, error) {
	switch message.Role {
	case llm.RoleSystem:
		return c.projectSystem(message, location)

	case llm.RoleUser:
		return c.projectUser(message, location)

	case llm.RoleAssistant:
		return c.projectAssistant(message, location)
	}

	return nil, invalidRequestError(
		location+".role: unknown role", nil,
	)
}

// projectSystem 投影系统消息。
// IR 保证系统消息只含文本；其它内容属于调用方绕过了校验。
func (c *Client) projectSystem(
	message *llm.Message,
	location string,
) (json.RawMessage, error) {
	texts := make([]string, 0, len(message.Parts))

	for index := range message.Parts {
		part := &message.Parts[index]

		if part.Kind != llm.PartText {
			return nil, invalidRequestError(fmt.Sprintf(
				"%s.parts[%d].kind: %q is not valid system content",
				location, index, part.Kind,
			), nil)
		}

		texts = append(texts, part.Text)
	}

	return encodeWireMessage(
		wireMessage{Role: "system", Content: strings.Join(texts, "")},
		"",
		"",
	)
}

// projectUser 投影用户消息。
func (c *Client) projectUser(
	message *llm.Message,
	location string,
) (json.RawMessage, error) {
	content := c.projectContent(message.Parts, location+".parts")

	return encodeWireMessage(
		wireMessage{Role: "user", Content: content},
		"",
		"",
	)
}

// projectAssistant 投影 assistant 消息。
//
// 多个文本块按原顺序拼接：wire 只有一个 content 字段，块边界无法表达。
// 推理内容按配置处理：声明支持推理字段时始终发送该字段（历史里没有推理
// 内容时发送空字符串），否则不发送。
func (c *Client) projectAssistant(
	message *llm.Message,
	location string,
) (json.RawMessage, error) {
	var (
		texts        []string
		reasoning    []string
		calls        []wireToolCall
		hasReasoning bool
	)

	for index := range message.Parts {
		part := &message.Parts[index]

		switch part.Kind {
		case llm.PartText:
			texts = append(texts, part.Text)

		case llm.PartReasoning:
			hasReasoning = true

			if c.reasoningField != "" {
				reasoning = append(reasoning, part.Text)
			}

		case llm.PartToolCall:
			calls = append(calls, wireToolCall{
				Type: "function",
				ID:   part.ToolCall.ID,
				Function: wireToolCallFunction{
					Name:      part.ToolCall.Name,
					Arguments: part.ToolCall.Arguments,
				},
			})

		default:
			c.drop(
				partLocation(location+".parts", index),
				fmt.Sprintf(
					"kind %q cannot appear in an assistant message",
					part.Kind,
				),
			)
		}
	}

	projected := wireMessage{Role: "assistant", ToolCalls: calls}

	switch {
	case len(texts) > 0:
		projected.Content = strings.Join(texts, "")

	case len(calls) > 0:
		// 只有工具调用：省略 content 键，避免被解读为空回答。

	default:
		projected.Content = ""
	}

	if c.reasoningField == "" {
		return encodeWireMessage(projected, "", "")
	}

	value := ""
	if hasReasoning {
		value = strings.Join(reasoning, "")
	}

	return encodeWireMessage(projected, c.reasoningField, value)
}

// projectToolFeedback 把一条工具反馈消息展开为多条 tool 消息。
// 顺序按调用在反馈消息中首次出现的位置确定。
func (c *Client) projectToolFeedback(
	message *llm.Message,
	location string,
) ([]json.RawMessage, error) {
	order := make([]string, 0, len(message.Parts))
	items := make(map[string][]wireContentPart, len(message.Parts))
	textOnly := make(map[string]bool, len(message.Parts))

	for index := range message.Parts {
		part := &message.Parts[index]
		callID := part.ResultOf.CallID
		partLocation := partLocation(location+".parts", index)

		if _, exists := items[callID]; !exists {
			order = append(order, callID)
			textOnly[callID] = true
		}

		switch part.Kind {
		case llm.PartEmpty:
			// 空结果：内容是空字符串，不追加任何内容。

		case llm.PartText, llm.PartJSON, llm.PartError:
			items[callID] = append(items[callID], wireContentPart{
				Type: "text",
				Text: feedbackText(part),
			})

		case llm.PartImage, llm.PartAudio:
			item, ok := c.projectContentPart(part, partLocation)
			if !ok {
				continue
			}

			textOnly[callID] = false
			items[callID] = append(items[callID], item)

		default:
			c.drop(partLocation, fmt.Sprintf(
				"kind %q has no chat completions equivalent",
				part.Kind,
			))
		}
	}

	encoded := make([]json.RawMessage, 0, len(order))

	for _, callID := range order {
		var content any

		if textOnly[callID] {
			texts := make([]string, 0, len(items[callID]))
			for _, item := range items[callID] {
				texts = append(texts, item.Text)
			}

			content = strings.Join(texts, "")
		} else {
			content = items[callID]
		}

		value, err := encodeWireMessage(
			wireMessage{
				Role:       "tool",
				Content:    content,
				ToolCallID: callID,
			},
			"",
			"",
		)
		if err != nil {
			return nil, err
		}

		encoded = append(encoded, value)
	}

	return encoded, nil
}

// projectContent 投影用户侧的全部内容块。
// 单一文本或 JSON 块使用字符串形式，兼容面最广。
// 无法表达的内容块被丢弃；全部被丢弃时返回空字符串。
func (c *Client) projectContent(
	parts []llm.Part,
	location string,
) any {
	if len(parts) == 1 {
		switch parts[0].Kind {
		case llm.PartText:
			return parts[0].Text

		case llm.PartJSON:
			return string(parts[0].JSON)
		}
	}

	content := make([]wireContentPart, 0, len(parts))

	for index := range parts {
		item, ok := c.projectContentPart(
			&parts[index],
			partLocation(location, index),
		)
		if !ok {
			continue
		}

		content = append(content, item)
	}

	if len(content) == 0 {
		return ""
	}

	return content
}

// projectContentPart 投影单个内容块。
// 返回 false 表示该内容块无法表达，已经丢弃并记录。
func (c *Client) projectContentPart(
	part *llm.Part,
	location string,
) (wireContentPart, bool) {
	switch part.Kind {
	case llm.PartText:
		return wireContentPart{Type: "text", Text: part.Text}, true

	case llm.PartJSON:
		return wireContentPart{
			Type: "text",
			Text: string(part.JSON),
		}, true

	case llm.PartImage:
		address, ok := c.imageURL(
			part.Image.Source,
			part.Image.MIMEType,
			location,
		)
		if !ok {
			return wireContentPart{}, false
		}

		return wireContentPart{
			Type:     "image_url",
			ImageURL: &wireImageURL{URL: address},
		}, true

	case llm.PartAudio:
		audio, ok := c.inputAudio(
			part.Audio.Source,
			part.Audio.MIMEType,
			location,
		)
		if !ok {
			return wireContentPart{}, false
		}

		return wireContentPart{
			Type:       "input_audio",
			InputAudio: audio,
		}, true
	}

	c.drop(location, fmt.Sprintf(
		"kind %q has no chat completions equivalent",
		part.Kind,
	))

	return wireContentPart{}, false
}

// imageURL 把图片来源投影为 Chat Completions 接受的地址。
// 资源库引用与供应商文件引用在本层无法解析，因此丢弃。
func (c *Client) imageURL(
	source llm.Source,
	mimeType string,
	location string,
) (string, bool) {
	switch source.Type {
	case llm.SourceData:
		return "data:" + mimeType + ";base64," +
			base64.StdEncoding.EncodeToString(source.Data), true

	case llm.SourceURL:
		return source.URL, true
	}

	c.drop(location, fmt.Sprintf(
		"image source %q cannot be resolved by this adapter",
		source.Type,
	))

	return "", false
}

// inputAudio 把音频来源投影为内联音频。
// input_audio 只接受 base64 字节与容器枚举，不接受 URL 或引用。
func (c *Client) inputAudio(
	source llm.Source,
	mimeType string,
	location string,
) (*wireInputAudio, bool) {
	if source.Type != llm.SourceData {
		c.drop(location, fmt.Sprintf(
			"audio source %q cannot be resolved by this adapter",
			source.Type,
		))

		return nil, false
	}

	format, ok := audioFormat(mimeType)
	if !ok {
		c.drop(location, fmt.Sprintf(
			"audio type %q has no container equivalent",
			mimeType,
		))

		return nil, false
	}

	return &wireInputAudio{
		Data:   base64.StdEncoding.EncodeToString(source.Data),
		Format: format,
	}, true
}

// audioFormat 把音频媒体类型换算为供应商的容器枚举。
func audioFormat(mimeType string) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return "", false
	}

	_, subtype, _ := strings.Cut(mediaType, "/")

	switch subtype {
	case "wav", "x-wav", "wave":
		return "wav", true

	case "mpeg", "mp3":
		return "mp3", true
	}

	return "", false
}

// feedbackText 返回工具结果内容块的文本形式。
func feedbackText(part *llm.Part) string {
	switch part.Kind {
	case llm.PartJSON:
		return string(part.JSON)

	case llm.PartError:
		if part.Error.Code == "" {
			return part.Error.Message
		}

		return part.Error.Code + ": " + part.Error.Message
	}

	return part.Text
}

// encodeWireMessage 把消息编码为 JSON 对象。
// reasoningField 非空时注入同名的推理字段。
func encodeWireMessage(
	message wireMessage,
	reasoningField string,
	reasoning string,
) (json.RawMessage, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, invalidRequestError("encode message", err)
	}

	if reasoningField == "" {
		return encoded, nil
	}

	fields, err := decodeObject(encoded)
	if err != nil {
		return nil, err
	}

	value, err := json.Marshal(reasoning)
	if err != nil {
		return nil, invalidRequestError("encode message reasoning", err)
	}

	fields[reasoningField] = value

	return encodeObject(fields)
}

// mergeExtraBody 把 ExtraBody 合并进已编码的请求体。
// 与适配器生成的字段冲突时报错：静默覆盖会让配置与 wire 内容不一致。
func (c *Client) mergeExtraBody(encoded []byte) ([]byte, error) {
	if len(c.extraBody) == 0 {
		return encoded, nil
	}

	fields, err := decodeObject(encoded)
	if err != nil {
		return nil, err
	}

	for key, value := range c.extraBody {
		if _, exists := fields[key]; exists {
			return nil, invalidRequestError(fmt.Sprintf(
				"extraBody[%q]: conflicts with a field produced by the adapter",
				key,
			), nil)
		}

		fields[key] = value
	}

	return encodeObject(fields)
}

// decodeObject 把 JSON 对象解码为字段表。
// 输入由本包编码，因此不重复检查键唯一性。
func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, invalidRequestError("decode request object", err)
	}

	return fields, nil
}

// encodeObject 重新编码字段表。
// map 的输出与迭代顺序无关，因此请求体是确定的。
func encodeObject(fields map[string]json.RawMessage) ([]byte, error) {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, invalidRequestError("encode request object", err)
	}

	return encoded, nil
}

// drop 记录一次内容降级：该内容块没有 wire 表达，因此没有发送。
// Logger 为空时完全静默。
func (c *Client) drop(location, reason string) {
	if c.logger == nil {
		return
	}

	c.logger.Warn(
		"chat completions: content dropped",
		"location", location,
		"reason", reason,
	)
}

// partLocation 拼接内容块的诊断位置。
func partLocation(parent string, index int) string {
	return fmt.Sprintf("%s[%d]", parent, index)
}
