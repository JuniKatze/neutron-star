package llm

import (
	"math"
	"testing"
)

// configTestFloat 返回指向指定浮点数的指针。
func configTestFloat(value float64) *float64 {
	return &value
}

// configTestInt 返回指向指定整数的指针。
func configTestInt(value int) *int {
	return &value
}

// TestConfigValidate 检查基础参数与子配置的校验。
func TestConfigValidate(t *testing.T) {
	valid := []struct {
		name   string
		config *Config
	}{
		{"nil config", nil},
		{"zero value", &Config{}},
		{"temperature zero", &Config{Temperature: configTestFloat(0)}},
		{"topP one", &Config{TopP: configTestFloat(1)}},
		{"topK one", &Config{TopK: configTestInt(1)}},
		{"max output tokens", &Config{MaxOutputTokens: configTestInt(1)}},
		{"stop with whitespace", &Config{Stop: []string{" ", "\n"}}},
		{
			"subconfigs",
			&Config{
				ToolChoice:     &ToolChoice{Mode: ToolChoiceAuto},
				Reasoning:      &Reasoning{Effort: ReasoningHigh},
				ResponseFormat: &ResponseFormat{Type: ResponseFormatText},
			},
		},
	}

	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			if err := test.config.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}

	invalid := []struct {
		name   string
		config *Config
	}{
		{"negative temperature", &Config{Temperature: configTestFloat(-0.1)}},
		{"NaN temperature", &Config{Temperature: configTestFloat(math.NaN())}},
		{"infinite temperature", &Config{Temperature: configTestFloat(math.Inf(1))}},
		{"zero topP", &Config{TopP: configTestFloat(0)}},
		{"topP above one", &Config{TopP: configTestFloat(1.5)}},
		{"zero topK", &Config{TopK: configTestInt(0)}},
		{"zero max output tokens", &Config{MaxOutputTokens: configTestInt(0)}},
		{"empty stop sequence", &Config{Stop: []string{""}}},
		{"invalid tool choice", &Config{ToolChoice: &ToolChoice{Mode: "bogus"}}},
		{"invalid reasoning", &Config{Reasoning: &Reasoning{Effort: "bogus"}}},
		{"invalid response format", &Config{ResponseFormat: &ResponseFormat{Type: "bogus"}}},
	}

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if test.config.Validate() == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}

// TestConfigCloneIsDeep 检查 Clone 不共享指针、切片与子配置。
func TestConfigCloneIsDeep(t *testing.T) {
	stop := []string{"END"}
	config := &Config{
		Temperature: configTestFloat(0.5),
		Stop:        stop,
		ToolChoice:  &ToolChoice{Mode: ToolChoiceAuto},
	}

	clone := config.Clone()

	*clone.Temperature = 1
	clone.Stop[0] = "STOP"
	clone.ToolChoice.Mode = ToolChoiceNone

	if *config.Temperature != 0.5 {
		t.Fatal("Clone shared scalar pointer")
	}
	if config.Stop[0] != "END" {
		t.Fatal("Clone shared Stop slice")
	}
	if config.ToolChoice.Mode != ToolChoiceAuto {
		t.Fatal("Clone shared subconfig")
	}

	empty := (&Config{Stop: []string{}}).Clone()
	if empty.Stop == nil || len(empty.Stop) != 0 {
		t.Fatal("Clone changed an empty Stop slice")
	}
	if (*Config)(nil).Clone() != nil {
		t.Fatal("Clone must preserve nil")
	}
}
