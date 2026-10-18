package llm

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// idTestFailingReader 返回读取错误。
type idTestFailingReader struct{}

func (idTestFailingReader) Read([]byte) (int, error) {
	return 0, errors.New("boom")
}

// TestNewID 检查标识的长度、字符集与随机性。
func TestNewID(t *testing.T) {
	first, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}

	if len(first) != 32 {
		t.Fatalf("length = %d, want 32", len(first))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("not hexadecimal: %v", err)
	}
	if first != strings.ToLower(first) {
		t.Fatal("identifier must be lowercase")
	}

	second, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	if first == second {
		t.Fatal("two identifiers are identical")
	}
}

// TestNewIDFromReader 检查随机源失败与短读。
func TestNewIDFromReader(t *testing.T) {
	if _, err := newIDFromReader(nil); err == nil {
		t.Fatal("accepted a nil reader")
	}
	if _, err := newIDFromReader(idTestFailingReader{}); err == nil {
		t.Fatal("accepted a failing reader")
	}
	if _, err := newIDFromReader(strings.NewReader("short")); err == nil {
		t.Fatal("accepted a short read")
	}

	value, err := newIDFromReader(strings.NewReader("0123456789abcdef"))
	if err != nil {
		t.Fatalf("newIDFromReader: %v", err)
	}
	if len(value) != 32 {
		t.Fatalf("length = %d, want 32", len(value))
	}
}
