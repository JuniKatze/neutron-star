package llm

import "testing"

// reasoningTestBool 返回指向指定布尔值的指针。
func reasoningTestBool(value bool) *bool {
	return &value
}

// reasoningTestInt 返回指向指定整数的指针。
func reasoningTestInt(value int) *int {
	return &value
}

// TestReasoningValidate 检查推理选项的组合规则。
func TestReasoningValidate(t *testing.T) {
	valid := []struct {
		name      string
		reasoning *Reasoning
	}{
		{"nil reasoning", nil},
		{"zero value", &Reasoning{}},
		{"enabled only", &Reasoning{Enabled: reasoningTestBool(true)}},
		{"disabled only", &Reasoning{Enabled: reasoningTestBool(false)}},
		{"effort none", &Reasoning{Effort: ReasoningNone}},
		{"effort high", &Reasoning{Effort: ReasoningHigh}},
		{"effort xhigh", &Reasoning{Effort: ReasoningXHigh}},
		{"effort max", &Reasoning{Effort: ReasoningMax}},
		{"budget with effort", &Reasoning{Effort: ReasoningLow, BudgetTokens: reasoningTestInt(100)}},
		{"summary with enabled", &Reasoning{Enabled: reasoningTestBool(true), Summary: reasoningTestBool(true)}},
		{"summary off while disabled", &Reasoning{Effort: ReasoningNone, Summary: reasoningTestBool(false)}},
	}

	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			if err := test.reasoning.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}

	invalid := []struct {
		name      string
		reasoning *Reasoning
	}{
		{"unknown effort", &Reasoning{Effort: "bogus"}},
		{"zero budget", &Reasoning{BudgetTokens: reasoningTestInt(0)}},
		{"negative budget", &Reasoning{BudgetTokens: reasoningTestInt(-1)}},
		{"none with enabled", &Reasoning{Effort: ReasoningNone, Enabled: reasoningTestBool(true)}},
		{"effort while disabled", &Reasoning{Effort: ReasoningHigh, Enabled: reasoningTestBool(false)}},
		{"budget while disabled", &Reasoning{Enabled: reasoningTestBool(false), BudgetTokens: reasoningTestInt(10)}},
		{"summary while disabled", &Reasoning{Effort: ReasoningNone, Summary: reasoningTestBool(true)}},
	}

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if test.reasoning.Validate() == nil {
				t.Fatal("accepted invalid reasoning")
			}
		})
	}
}

// TestReasoningCloneIsDeep 检查 Clone 不共享指针字段。
func TestReasoningCloneIsDeep(t *testing.T) {
	reasoning := &Reasoning{
		Enabled:      reasoningTestBool(true),
		BudgetTokens: reasoningTestInt(100),
		Summary:      reasoningTestBool(true),
	}

	clone := reasoning.Clone()

	*clone.Enabled = false
	*clone.BudgetTokens = 1
	*clone.Summary = false

	if !*reasoning.Enabled || *reasoning.BudgetTokens != 100 || !*reasoning.Summary {
		t.Fatal("Clone shared pointer fields")
	}
	if (*Reasoning)(nil).Clone() != nil {
		t.Fatal("Clone must preserve nil")
	}
}
