package llm

import "testing"

// TestClonePtr 检查指针复制保留 nil、且不与原指针共享内存。
func TestClonePtr(t *testing.T) {
	if clonePtr[int](nil) != nil {
		t.Fatal("Clone of nil must be nil")
	}

	value := 7
	clone := clonePtr(&value)

	if clone == &value {
		t.Fatal("clone shares the original pointer")
	}
	if *clone != 7 {
		t.Fatalf("clone = %d, want 7", *clone)
	}

	*clone = 9
	if value != 7 {
		t.Fatal("changing the clone changed the original")
	}
}
