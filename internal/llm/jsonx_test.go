package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestValidateValue 检查完整性、重复对象键与数字原文。
func TestValidateValue(t *testing.T) {
	valid := []string{
		`{}`,
		`null`,
		`[]`,
		`[1,2,{"k":null}]`,
		`{"n":9007199254740993}`,
		" \n {\"a\":1}\t ",
	}
	for _, input := range valid {
		if err := validateValue([]byte(input)); err != nil {
			t.Errorf("rejected %q: %v", input, err)
		}
	}

	invalid := []string{
		``,
		`   `,
		`{`,
		`{"a":1,}`,
		`{"a":1,"a":2}`,
		`{"a":{"b":1,"b":2}}`,
		`[{"a":1,"a":2}]`,
		`{"a":}`,
	}
	for _, input := range invalid {
		if validateValue([]byte(input)) == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

// TestValidateObject 检查根值必须是对象。
func TestValidateObject(t *testing.T) {
	if err := validateObject([]byte(`{"a":1}`)); err != nil {
		t.Fatalf("rejected object: %v", err)
	}

	for _, input := range []string{`[]`, `1`, `null`, `"x"`, `{"a":1,"a":2}`} {
		if validateObject([]byte(input)) == nil {
			t.Errorf("accepted non-object %q", input)
		}
	}
}

// TestDigestProperties 检查摘要的格式、确定性与归一化规则。
func TestDigestProperties(t *testing.T) {
	first, err := digest("llm.part", map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	if !strings.HasPrefix(first, "sha256:") {
		t.Fatalf("digest = %q, want sha256: prefix", first)
	}
	if len(first) != len("sha256:")+64 {
		t.Fatalf("digest length = %d", len(first))
	}

	again, err := digest("llm.part", map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if first != again {
		t.Fatal("digest is not deterministic")
	}

	reordered, err := digest("llm.part", map[string]any{"b": 2, "a": 1})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if first != reordered {
		t.Fatal("object key order changed the digest")
	}

	otherDomain, err := digest("llm.message", map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if first == otherDomain {
		t.Fatal("domain did not separate the digests")
	}

	raw, err := digest("llm.part", map[string]any{"json": json.RawMessage(`{"k":1,"j":2}`)})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	rawReordered, err := digest("llm.part", map[string]any{"json": json.RawMessage(`{"j":2,"k":1}`)})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if raw != rawReordered {
		t.Fatal("RawMessage object keys were not normalized")
	}

	// 大整数必须保留原文：float64 会把这两个数变成同一个值。
	bigA, err := digest("llm.part", map[string]any{"json": json.RawMessage(`{"n":9007199254740993}`)})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	bigB, err := digest("llm.part", map[string]any{"json": json.RawMessage(`{"n":9007199254740992}`)})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if bigA == bigB {
		t.Fatal("large integers were rounded before hashing")
	}
}
