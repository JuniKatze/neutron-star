package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ResponseFormatType 表示生成结果的格式要求。
type ResponseFormatType string

const (
	// ResponseFormatText 请求普通文本输出。
	ResponseFormatText ResponseFormatType = "text"

	// ResponseFormatJSONObject 请求 JSON 对象输出。
	ResponseFormatJSONObject ResponseFormatType = "json_object"

	// ResponseFormatJSONSchema 请求符合指定 Schema 的输出。
	ResponseFormatJSONSchema ResponseFormatType = "json_schema"
)

// ResponseFormat 描述生成结果的格式约束。
// nil 表示未指定。非 nil ResponseFormat 必须指定合法 Type；
// ResponseFormat 的零值无效。
type ResponseFormat struct {
	// Type 指定输出格式。
	Type ResponseFormatType `json:"type"`

	// Name 是 json_schema 模式下 Schema 的非空名称。
	// 其他模式必须保持为空。
	Name string `json:"name,omitempty"`

	// Description 是 json_schema 模式下 Schema 的可选说明。
	// 它向模型解释这份 Schema 的用途，不参与 Schema 校验。
	// 其他模式必须保持为空。
	Description string `json:"description,omitempty"`

	// Schema 保存 json_schema 模式下的原始 Schema 文档。
	// 本包要求文档为 JSON 对象，并拒绝重复对象键。
	// 此要求不限制 Schema 所描述的输出类型必须是对象。
	Schema json.RawMessage `json:"schema,omitempty"`

	// Strict 控制是否请求严格遵守 Schema。
	// nil 表示未指定。仅 json_schema 模式允许设置此字段。
	Strict *bool `json:"strict,omitempty"`
}

// Validate 检查 f 的格式类型、字段组合及 Schema 的 JSON 结构。
// f 为 nil 时返回 nil。本方法不修改 f。
//
// text 和 json_object 模式不允许设置 Name、Description、Schema 或
// Strict，即使 Strict 指向 false 也属于已设置。
//
// Validate 不编译 JSON Schema，不验证完整的 Schema 语义，
// 不解析外部引用，也不检查供应商支持的 Schema 子集。
func (f *ResponseFormat) Validate() error {
	if f == nil {
		return nil
	}

	switch f.Type {
	case ResponseFormatText, ResponseFormatJSONObject:
		if f.Name != "" {
			return errors.New("name: only valid in json_schema mode")
		}
		if f.Description != "" {
			return errors.New(
				"description: only valid in json_schema mode",
			)
		}
		if f.Schema != nil {
			return errors.New("schema: only valid in json_schema mode")
		}
		if f.Strict != nil {
			return errors.New("strict: only valid in json_schema mode")
		}

	case ResponseFormatJSONSchema:
		if strings.TrimSpace(f.Name) == "" {
			return errors.New("name: required in json_schema mode")
		}

		if strings.TrimSpace(f.Name) != f.Name {
			return errors.New(
				"name: must not have leading or trailing whitespace",
			)
		}

		if err := validateObject(f.Schema); err != nil {
			return fmt.Errorf("schema: %w", err)
		}

	default:
		return fmt.Errorf(
			"type: unknown response format type %q",
			f.Type,
		)
	}

	return nil
}

// Clone 返回 f 的深拷贝。
// 返回值不与 f 共享 Schema 的底层数组或 Strict 指针。
// f 为 nil 时返回 nil。Clone 保留 Schema 原文及 nil 状态，不校验配置。
func (f *ResponseFormat) Clone() *ResponseFormat {
	if f == nil {
		return nil
	}

	out := *f
	out.Strict = clonePtr(f.Strict)

	if f.Schema != nil {
		out.Schema = make(json.RawMessage, len(f.Schema))
		copy(out.Schema, f.Schema)
	}

	return &out
}
