package llm

import (
	"errors"
	"fmt"
	"math"
)

// Config 控制一次模型生成的行为。
// Config 的零值有效，表示不显式设置任何生成选项。
// nil Config 具有相同的未设置语义。
//
// Validate 和 Clone 不修改 Config。调用方必须保证调用期间
// 没有其他 goroutine 并发修改其字段或引用的数据。
type Config struct {
	// Temperature 控制采样温度。
	// nil 表示未指定；已设置的值必须有限且非负。
	// 具体上限由适配器检查。
	Temperature *float64 `json:"temperature,omitempty"`

	// TopP 控制核采样概率阈值。
	// nil 表示未指定；已设置的值必须在 (0, 1] 内。
	TopP *float64 `json:"topP,omitempty"`

	// TopK 限制采样候选数量。
	// nil 表示未指定；已设置的值必须为正数。
	TopK *int `json:"topK,omitempty"`

	// MaxOutputTokens 指定输出 token 上限。
	// nil 表示未指定；已设置的值必须为正数。
	MaxOutputTokens *int `json:"maxOutputTokens,omitempty"`

	// Stop 保存停止序列。
	// 序列不得为空字符串，但可以包含空白字符。
	// nil 和空切片均表示没有显式停止序列。
	Stop []string `json:"stop,omitempty"`

	// ToolChoice 控制模型如何选择工具。
	ToolChoice *ToolChoice `json:"toolChoice,omitempty"`

	// Reasoning 控制推理相关选项。
	Reasoning *Reasoning `json:"reasoning,omitempty"`

	// ResponseFormat 控制生成结果的格式。
	ResponseFormat *ResponseFormat `json:"responseFormat,omitempty"`
}

// Validate 检查 c 的基础参数及子配置。
// c 为 nil 时返回 nil。本方法不修改 c，也不设置默认值。
//
// Validate 不检查目标模型的参数支持情况，不规定推理预算
// 与输出上限的关系，也不检查 ToolChoice 引用的工具是否已声明。
// 工具声明关联由 Request.Validate 检查。
func (c *Config) Validate() error {
	if c == nil {
		return nil
	}

	if c.Temperature != nil {
		value := *c.Temperature
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return errors.New(
				"temperature: must be finite and non-negative",
			)
		}
	}

	if c.TopP != nil {
		value := *c.TopP
		if math.IsNaN(value) || math.IsInf(value, 0) ||
			value <= 0 || value > 1 {
			return errors.New("topP: must be finite and in (0, 1]")
		}
	}

	if c.TopK != nil && *c.TopK <= 0 {
		return errors.New("topK: must be positive")
	}

	if c.MaxOutputTokens != nil && *c.MaxOutputTokens <= 0 {
		return errors.New("maxOutputTokens: must be positive")
	}

	for index, stop := range c.Stop {
		if stop == "" {
			return fmt.Errorf("stop[%d]: must not be empty", index)
		}
	}

	if err := c.ToolChoice.Validate(); err != nil {
		return fmt.Errorf("toolChoice: %w", err)
	}

	if err := c.Reasoning.Validate(); err != nil {
		return fmt.Errorf("reasoning: %w", err)
	}

	if err := c.ResponseFormat.Validate(); err != nil {
		return fmt.Errorf("responseFormat: %w", err)
	}

	return nil
}

// Clone 返回 c 的深拷贝。
// 返回值不与 c 共享标量指针、Stop 的底层数组或子配置中的可变内存。
// c 为 nil 时返回 nil。Clone 不校验或修复配置。
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}

	out := *c
	out.Temperature = clonePtr(c.Temperature)
	out.TopP = clonePtr(c.TopP)
	out.TopK = clonePtr(c.TopK)
	out.MaxOutputTokens = clonePtr(c.MaxOutputTokens)

	if c.Stop != nil {
		out.Stop = make([]string, len(c.Stop))
		copy(out.Stop, c.Stop)
	}

	out.ToolChoice = c.ToolChoice.Clone()
	out.Reasoning = c.Reasoning.Clone()
	out.ResponseFormat = c.ResponseFormat.Clone()

	return &out
}
