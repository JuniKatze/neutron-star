package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PartKind 表示一个内容块的载荷类型。
type PartKind string

const (
	// PartText 表示普通文本。
	PartText PartKind = "text"

	// PartReasoning 表示与普通文本分离的推理内容。
	PartReasoning PartKind = "reasoning"

	// PartRefusal 表示模型拒绝执行时给出的说明文本。
	PartRefusal PartKind = "refusal"

	// PartJSON 表示一个完整 JSON 值。
	PartJSON PartKind = "json"

	// PartToolCall 表示模型提出的函数工具调用。
	PartToolCall PartKind = "tool_call"

	// PartImage 表示图片内容。
	PartImage PartKind = "image"

	// PartAudio 表示音频内容。
	PartAudio PartKind = "audio"

	// PartVideo 表示视频内容。
	PartVideo PartKind = "video"

	// PartDocument 表示文档内容。
	PartDocument PartKind = "document"

	// PartFile 表示普通文件引用。
	PartFile PartKind = "file"

	// PartError 表示工具结果中的错误内容。
	PartError PartKind = "error"

	// PartEmpty 表示没有返回内容的工具结果。
	PartEmpty PartKind = "empty"

	// PartOpaque 表示仅通过 Replay 保存的私有内容。
	PartOpaque PartKind = "opaque"
)

// Part 表示一个非递归内容块。
//
// Kind 决定唯一有效载荷：Text、Reasoning 与 Refusal 共用 Text 载荷；
// Empty 与 Opaque 没有公开载荷。Part 的零值无效。
//
// ResultOf 关联工具结果，不表示执行状态。
// Replay 保存供应商私有状态，不属于公开内容载荷。
//
// 调用方法期间，调用方必须保证没有其他 goroutine
// 并发修改 Part 或其引用的数据。
type Part struct {
	// Kind 指定内容类型。
	Kind PartKind `json:"kind"`

	// Text 保存 text、reasoning 或 refusal 内容。
	Text string `json:"text,omitempty"`

	// JSON 保存原始 JSON 值。
	// nil 表示未设置，字面量 null 是有效载荷。
	JSON json.RawMessage `json:"json,omitempty"`

	// Image 保存 image 载荷。
	Image *Image `json:"image,omitempty"`

	// Audio 保存 audio 载荷。
	Audio *Audio `json:"audio,omitempty"`

	// Video 保存 video 载荷。
	Video *Video `json:"video,omitempty"`

	// Document 保存 document 载荷。
	Document *Document `json:"document,omitempty"`

	// File 保存 file 载荷。
	File *File `json:"file,omitempty"`

	// ToolCall 保存 tool_call 载荷。
	ToolCall *ToolCall `json:"toolCall,omitempty"`

	// Error 保存 error 载荷。
	Error *ToolError `json:"error,omitempty"`

	// ResultOf 将被动内容关联到工具调用。
	ResultOf *ToolResultRef `json:"resultOf,omitempty"`

	// Replay 保存与本内容块绑定的私有回放状态。
	Replay *Replay `json:"replay,omitempty"`
}

// Validate 检查 p 的载荷、结果关联及回放绑定。
// 本方法不修改 p，不执行工具，也不获取媒体资源。
//
// Validate 不检查函数参数是否为 JSON 对象或符合参数 Schema。
// 它不检查对应调用是否存在，也不检查 Replay 是否匹配目标路由；
// 这些检查分别由执行边界、请求和适配器负责。
func (p Part) Validate() error {
	if err := p.validateContent(); err != nil {
		return err
	}

	switch p.Kind {
	case PartText, PartReasoning, PartRefusal:
		if p.Text == "" {
			return errors.New("text: must not be empty")
		}

	case PartOpaque:
		if p.Replay == nil {
			return errors.New("replay: required for opaque content")
		}
	}

	if p.Replay != nil {
		if err := p.Replay.Validate(); err != nil {
			return fmt.Errorf("replay: %w", err)
		}

		binding, err := p.Binding()
		if err != nil {
			return fmt.Errorf("replay binding: %w", err)
		}

		if p.Replay.Binding != binding {
			return errors.New(
				"replay.binding: does not match part content",
			)
		}
	}

	return nil
}

// validateContent 检查公开内容与关联字段，不检查 Replay。
// Binding 使用该方法，避免回放校验与摘要计算相互递归。
func (p Part) validateContent() error {
	if err := p.validatePayloadFields(); err != nil {
		return err
	}

	switch p.Kind {
	case PartText, PartReasoning, PartRefusal:
		// 文本必须非空的检查由 Validate 负责。

	case PartJSON:
		if err := validateValue(p.JSON); err != nil {
			return fmt.Errorf("json: %w", err)
		}

	case PartToolCall:
		if err := p.ToolCall.Validate(); err != nil {
			return fmt.Errorf("toolCall: %w", err)
		}

	case PartImage:
		if err := p.Image.Validate(); err != nil {
			return fmt.Errorf("image: %w", err)
		}

	case PartAudio:
		if err := p.Audio.Validate(); err != nil {
			return fmt.Errorf("audio: %w", err)
		}

	case PartVideo:
		if err := p.Video.Validate(); err != nil {
			return fmt.Errorf("video: %w", err)
		}

	case PartDocument:
		if err := p.Document.Validate(); err != nil {
			return fmt.Errorf("document: %w", err)
		}

	case PartFile:
		if err := p.File.Validate(); err != nil {
			return fmt.Errorf("file: %w", err)
		}

	case PartError:
		if err := p.Error.Validate(); err != nil {
			return fmt.Errorf("error: %w", err)
		}

	case PartEmpty, PartOpaque:
		// 这两种类型没有公开载荷。

	default:
		return fmt.Errorf("kind: unknown part kind %q", p.Kind)
	}

	switch p.Kind {
	case PartReasoning, PartRefusal, PartToolCall, PartOpaque:
		if p.ResultOf != nil {
			return fmt.Errorf(
				"resultOf: not allowed for part kind %q",
				p.Kind,
			)
		}

	case PartError, PartEmpty:
		if p.ResultOf == nil {
			return fmt.Errorf(
				"resultOf: required for part kind %q",
				p.Kind,
			)
		}
	}

	if p.ResultOf != nil {
		if err := p.ResultOf.Validate(); err != nil {
			return fmt.Errorf("resultOf: %w", err)
		}
	}

	return nil
}

// validatePayloadFields 拒绝与 Kind 不匹配的非空载荷。
// 指针或 RawMessage 非 nil 即视为已设置，包括非 nil 空切片。
func (p Part) validatePayloadFields() error {
	fields := []struct {
		name    string
		present bool
		allowed bool
	}{
		{
			"text",
			p.Text != "",
			p.Kind == PartText ||
				p.Kind == PartReasoning ||
				p.Kind == PartRefusal,
		},
		{"json", p.JSON != nil, p.Kind == PartJSON},
		{"image", p.Image != nil, p.Kind == PartImage},
		{"audio", p.Audio != nil, p.Kind == PartAudio},
		{"video", p.Video != nil, p.Kind == PartVideo},
		{"document", p.Document != nil, p.Kind == PartDocument},
		{"file", p.File != nil, p.Kind == PartFile},
		{"toolCall", p.ToolCall != nil, p.Kind == PartToolCall},
		{"error", p.Error != nil, p.Kind == PartError},
	}

	for _, field := range fields {
		if field.present && !field.allowed {
			return fmt.Errorf(
				"%s: incompatible with part kind %q",
				field.name,
				p.Kind,
			)
		}
	}

	return nil
}

// Clone 返回 p 的深拷贝。
// 返回值不与 p 共享载荷、结果关联或回放中的可变内存。
// Clone 保留 nil 和原始内容，不校验或修复非法组合。
func (p Part) Clone() Part {
	out := p

	if p.JSON != nil {
		out.JSON = make(json.RawMessage, len(p.JSON))
		copy(out.JSON, p.JSON)
	}

	out.Image = p.Image.Clone()
	out.Audio = p.Audio.Clone()
	out.Video = p.Video.Clone()
	out.Document = p.Document.Clone()
	out.File = p.File.Clone()

	if p.ToolCall != nil {
		call := *p.ToolCall
		out.ToolCall = &call
	}

	if p.Error != nil {
		value := *p.Error
		out.Error = &value
	}

	if p.ResultOf != nil {
		ref := *p.ResultOf
		out.ResultOf = &ref
	}

	out.Replay = p.Replay.Clone()
	return out
}

// partBindingValue 构造 Part 的摘要输入：Binding 只对这些字段算摘要。
//
// 调用方必须已经通过 validateContent。未知种类不会到达这里：Binding 先执行
// 该检查，未知种类在那里已被拒绝。因此本函数不返回错误；万一出现未知种类，
// 摘要输入退化为只含 kind 的对象，保证摘要仍然是确定的。
//
// 添加或修改 Part 的字段时，必须同步这里。漏掉的字段不会报错，只会让摘要
// 少算内容——两个不同的内容会得到同一个摘要，回放校验于是在该失败时通过。
func partBindingValue(p Part) map[string]any {
	value := map[string]any{
		"kind": string(p.Kind),
	}

	if p.ResultOf != nil {
		value["resultOf"] = map[string]any{
			"callId": p.ResultOf.CallID,
		}
	}

	switch p.Kind {
	case PartText, PartReasoning, PartRefusal:
		value["text"] = p.Text

	case PartJSON:
		value["json"] = p.JSON

	case PartToolCall:
		value["toolCall"] = map[string]any{
			"id":        p.ToolCall.ID,
			"name":      p.ToolCall.Name,
			"arguments": p.ToolCall.Arguments,
		}

	case PartImage:
		value["image"] = mediaBindingValue(p.Image.Source, p.Image.MIMEType)

	case PartAudio:
		value["audio"] = mediaBindingValue(p.Audio.Source, p.Audio.MIMEType)

	case PartVideo:
		value["video"] = mediaBindingValue(p.Video.Source, p.Video.MIMEType)

	case PartDocument:
		document := mediaBindingValue(
			p.Document.Source,
			p.Document.MIMEType,
		)
		document["name"] = p.Document.Name
		document["title"] = p.Document.Title
		document["context"] = p.Document.Context

		if p.Document.Citations != nil {
			document["citations"] = *p.Document.Citations
		}

		value["document"] = document

	case PartFile:
		file := mediaBindingValue(p.File.Source, p.File.MIMEType)
		file["name"] = p.File.Name
		value["file"] = file

	case PartError:
		value["error"] = map[string]any{
			"code":    p.Error.Code,
			"message": p.Error.Message,
		}

	case PartEmpty, PartOpaque:
		// 这两种类型没有公开载荷，摘要输入只有 kind 与 resultOf。

	default:
		// 未知种类：只保留 kind，见函数注释。

	}

	return value
}

// mediaBindingValue 构造媒体来源与媒体类型的摘要输入。
func mediaBindingValue(source Source, mimeType string) map[string]any {
	return map[string]any{
		"source":   sourceBindingValue(source),
		"mimeType": mimeType,
	}
}

// sourceBindingValue 构造来源的摘要输入。
func sourceBindingValue(source Source) map[string]any {
	value := map[string]any{
		"type": string(source.Type),
	}

	switch source.Type {
	case SourceData:
		value["data"] = source.Data

	case SourceURL:
		value["url"] = source.URL

	case SourceAsset:
		value["assetId"] = source.AssetID

	case SourceProvider:
		value["provider"] = map[string]any{
			"adapter":    source.Provider.Adapter,
			"connection": source.Provider.Connection,
			"id":         source.Provider.ID,
		}

	default:
		// 未知类型：只保留 type 这一项。
	}

	return value
}

// Binding 返回 p 的公开内容摘要，不包含 Replay。
// 摘要包含载荷及 ResultOf，并使用 llm.part 域区分对象类型。
//
// Binding 只检查公开内容结构：
// Replay 是否存在、是否匹配目标路由都不在它的职责内。
// 因此计算成功不代表 Validate 成功。
// 本方法不修改 p。
func (p Part) Binding() (string, error) {
	if err := p.validateContent(); err != nil {
		return "", err
	}

	return digest("llm.part", partBindingValue(p))
}

// ToolCall 表示模型提出的 JSON 对象参数函数调用。
// 它保存原始调用信息，不保存执行状态。
// ToolCall 的零值无效。
type ToolCall struct {
	// ID 是关联结果使用的唯一调用标识。
	ID string `json:"id"`

	// Name 是模型请求调用的工具名称。
	Name string `json:"name"`

	// Arguments 保存原始参数字符串。
	// 它可能不是合法 JSON；执行前必须另行检查。
	Arguments string `json:"arguments"`
}

// Validate 检查 c 的调用标识及工具名称。
// c 为 nil 时返回错误。本方法不修改 c，也不解析 Arguments。
func (c *ToolCall) Validate() error {
	if c == nil {
		return errors.New("tool call is nil")
	}
	if strings.TrimSpace(c.ID) == "" {
		return errors.New("id: required")
	}
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("name: required")
	}

	return nil
}

// ValidateArgumentsObject 检查调用结构及参数是否为完整 JSON 对象。
//
// 它拒绝重复对象键，不修改参数原文，不执行参数 Schema 校验或工具调用。
// 本包不在生成路径上调用它：参数原文可能不是合法 JSON，而保存模型实际
// 返回的内容不该因此失败。它供执行边界在真正调用工具之前使用。
func (c *ToolCall) ValidateArgumentsObject() error {
	if err := c.Validate(); err != nil {
		return err
	}

	if err := validateObject([]byte(c.Arguments)); err != nil {
		return fmt.Errorf("arguments: %w", err)
	}

	return nil
}

// ToolResultRef 将一个被动内容块关联到此前的工具调用。
// 它不保存执行状态或结果内容。ToolResultRef 的零值无效。
type ToolResultRef struct {
	// CallID 是对应 ToolCall 的唯一标识。
	CallID string `json:"callId"`
}

// Validate 检查 r 是否包含非空调用标识。
// r 为 nil 时返回错误。本方法不修改 r。
// 对应调用的存在性及结果分组由 Request.Validate 检查。
func (r *ToolResultRef) Validate() error {
	if r == nil {
		return errors.New("resultOf is nil")
	}
	if strings.TrimSpace(r.CallID) == "" {
		return errors.New("callId: required")
	}

	return nil
}

// ToolError 表示反馈给模型的工具错误内容。
// 它不表示模型传输错误，也不承担工具执行状态机。
// ToolError 的零值无效。
type ToolError struct {
	// Code 是可选的机器可读错误标识。
	Code string `json:"code,omitempty"`

	// Message 是非空的错误说明。
	Message string `json:"message"`
}

// Validate 检查 e 的错误说明及已设置的错误标识。
// e 为 nil 时返回错误。本方法不修改 e，也不限制 Code 的词汇表。
func (e *ToolError) Validate() error {
	if e == nil {
		return errors.New("tool error is nil")
	}
	if strings.TrimSpace(e.Message) == "" {
		return errors.New("message: required")
	}
	if e.Code != "" && strings.TrimSpace(e.Code) == "" {
		return errors.New("code: must not be blank when set")
	}

	return nil
}
