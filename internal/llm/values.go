package llm

// clonePtr 复制 value 指向的值，并保留 nil。
// 它不递归复制值中的引用，本包使用它复制标量选项。
func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}

	out := *value
	return &out
}
