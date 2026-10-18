package llm

import "testing"

// TestSourceURLRejectsUserInfo 检查 URL 来源拒绝内嵌凭证，但保留查询串凭证。
func TestSourceURLRejectsUserInfo(t *testing.T) {
	rejected := []string{
		"https://alice:hunter2@example.com/x.png",
		"https://alice@example.com/x.png",
	}
	for _, raw := range rejected {
		source := Source{Type: SourceURL, URL: raw}
		if err := source.Validate(); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}

	accepted := []string{
		"https://example.com/x.png",
		// 预签名 URL：凭证在查询串里，不属于 userinfo
		"https://bucket.s3.amazonaws.com/x.png?X-Amz-Signature=9f3a",
	}
	for _, raw := range accepted {
		source := Source{Type: SourceURL, URL: raw}
		if err := source.Validate(); err != nil {
			t.Errorf("rejected %q: %v", raw, err)
		}
	}
}
