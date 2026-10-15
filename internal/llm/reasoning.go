package llm

import (
	"errors"
	"fmt"
)

// ReasoningEffort 表示声明的推理强度。
// 不同供应商对相同强度的实现不保证具有相同计算预算。
type ReasoningEffort string

const (
	// ReasoningNone 表示明确不使用推理。
	ReasoningNone ReasoningEffort = "none"

	// ReasoningMinimal 表示最低推理强度。
	ReasoningMinimal ReasoningEffort = "minimal"

	// ReasoningLow 表示低推理强度。
	ReasoningLow ReasoningEffort = "low"

	// ReasoningMedium 表示中等推理强度。
	ReasoningMedium ReasoningEffort = "medium"

	// ReasoningHigh 表示高推理强度。
	ReasoningHigh ReasoningEffort = "high"

	// ReasoningXHigh 表示高于 high 的推理强度。
	ReasoningXHigh ReasoningEffort = "xhigh"

	// ReasoningMax 表示供应商支持的最高推理强度。
	ReasoningMax ReasoningEffort = "max"
)

// Reasoning 控制推理相关选项。
// nil 和 Reasoning 的零值均表示没有显式推理设置。
//
// 选项不必同时设置。未指定 Enabled 时，可以单独指定其他选项；
// 适配器必须检查目标模型能否表达该组合，不得静默丢弃选项。
type Reasoning struct {
	// Enabled 控制是否启用推理。
	// nil 表示未指定，true 表示启用，false 表示关闭。
	Enabled *bool `json:"enabled"`

	// Effort 指定推理强度，空字符串表示未指定。
	// none 表示关闭推理，与 Enabled=true 冲突。
	Effort ReasoningEffort `json:"effort,omitempty"`

	// BudgetTokens 指定推理 token 预算。
	// nil 表示未指定；已设置的值必须为正数。
	BudgetTokens *int `json:"budgetTokens,omitempty"`

	// Summary 控制是否请求推理摘要。
	// nil 表示未指定，false 表示明确关闭摘要。
	Summary *bool `json:"summary,omitempty"`
}

// Validate 检查 r 的强度、预算及选项组合。
// r 为 nil 时返回 nil。本方法不修改 r，也不推导 Enabled。
//
// 明确关闭推理时，不允许有效推理强度、预算或 Summary=true。
// Summary=false 不表示启用推理，因此允许与关闭设置同时存在。
// 目标模型的预算范围和支持能力由适配器检查。
func (r *Reasoning) Validate() error {
	if r == nil {
		return nil
	}

	switch r.Effort {
	case "",
		ReasoningNone,
		ReasoningMinimal,
		ReasoningLow,
		ReasoningMedium,
		ReasoningHigh,
		ReasoningXHigh,
		ReasoningMax:
	default:
		return fmt.Errorf(
			"effort: unknown reasoning effort %q",
			r.Effort,
		)
	}

	if r.BudgetTokens != nil && *r.BudgetTokens <= 0 {
		return errors.New("budgetTokens: must be positive")
	}

	if r.Enabled != nil && *r.Enabled && r.Effort == ReasoningNone {
		return errors.New("effort: none conflicts with enabled=true")
	}

	disabled := r.Effort == ReasoningNone ||
		(r.Enabled != nil && !*r.Enabled)

	if disabled {
		if r.Effort != "" && r.Effort != ReasoningNone {
			return errors.New(
				"effort: active effort conflicts with disabled reasoning",
			)
		}
		if r.BudgetTokens != nil {
			return errors.New(
				"budgetTokens: not allowed when reasoning is disabled",
			)
		}
		if r.Summary != nil && *r.Summary {
			return errors.New(
				"summary: cannot be enabled when reasoning is disabled",
			)
		}
	}

	return nil
}

// Clone 返回 r 的深拷贝。
// 返回值不与 r 共享 Enabled、BudgetTokens 或 Summary 指针。
// r 为 nil 时返回 nil。Clone 不校验配置。
func (r *Reasoning) Clone() *Reasoning {
	if r == nil {
		return nil
	}

	out := *r
	out.Enabled = clonePtr(r.Enabled)
	out.BudgetTokens = clonePtr(r.BudgetTokens)
	out.Summary = clonePtr(r.Summary)

	return &out
}
