package llm

import "testing"

// toolChoiceTestBool 返回指向指定布尔值的指针。
func toolChoiceTestBool(value bool) *bool {
	return &value
}

// TestToolChoiceValidate 检查模式、工具名与并行开关的组合。
func TestToolChoiceValidate(t *testing.T) {
	valid := []struct {
		name   string
		choice *ToolChoice
	}{
		{"nil choice", nil},
		{"auto", &ToolChoice{Mode: ToolChoiceAuto}},
		{"none", &ToolChoice{Mode: ToolChoiceNone}},
		{"required", &ToolChoice{Mode: ToolChoiceRequired}},
		{"named", &ToolChoice{Mode: ToolChoiceNamed, Name: "read_file"}},
		{"auto with parallel", &ToolChoice{Mode: ToolChoiceAuto, Parallel: toolChoiceTestBool(true)}},
		{"required with parallel off", &ToolChoice{Mode: ToolChoiceRequired, Parallel: toolChoiceTestBool(false)}},
		{"named with parallel", &ToolChoice{Mode: ToolChoiceNamed, Name: "read_file", Parallel: toolChoiceTestBool(true)}},
		{"none with parallel off", &ToolChoice{Mode: ToolChoiceNone, Parallel: toolChoiceTestBool(false)}},
	}

	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			if err := test.choice.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}

	invalid := []struct {
		name   string
		choice *ToolChoice
	}{
		{"zero value", &ToolChoice{}},
		{"unknown mode", &ToolChoice{Mode: "bogus"}},
		{"auto with name", &ToolChoice{Mode: ToolChoiceAuto, Name: "read_file"}},
		{"none with name", &ToolChoice{Mode: ToolChoiceNone, Name: "read_file"}},
		{"required with name", &ToolChoice{Mode: ToolChoiceRequired, Name: "read_file"}},
		{"named without name", &ToolChoice{Mode: ToolChoiceNamed}},
		{"named with blank name", &ToolChoice{Mode: ToolChoiceNamed, Name: " "}},
		{"named with whitespace", &ToolChoice{Mode: ToolChoiceNamed, Name: " read_file "}},
		{"none with parallel on", &ToolChoice{Mode: ToolChoiceNone, Parallel: toolChoiceTestBool(true)}},
	}

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if test.choice.Validate() == nil {
				t.Fatal("accepted invalid tool choice")
			}
		})
	}
}

// TestToolChoiceCloneIsDeep 检查 Clone 不共享 Parallel 指针。
func TestToolChoiceCloneIsDeep(t *testing.T) {
	choice := &ToolChoice{Mode: ToolChoiceAuto, Parallel: toolChoiceTestBool(true)}

	clone := choice.Clone()
	*clone.Parallel = false

	if !*choice.Parallel {
		t.Fatal("Clone shared parallel pointer")
	}
	if (*ToolChoice)(nil).Clone() != nil {
		t.Fatal("Clone must preserve nil")
	}
}
