package llm

import (
	"errors"
	"testing"
)

// TestErrorText 检查诊断文本的取值优先级。
func TestErrorText(t *testing.T) {
	cause := errors.New("dial failed")

	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{"nil error", nil, "<nil>"},
		{"zero value", &Error{}, "llm error"},
		{"kind only", &Error{Kind: ErrorProtocol}, "llm protocol"},
		{"message", &Error{Kind: ErrorProtocol, Message: "broken stream"}, "llm protocol: broken stream"},
		{"message without kind", &Error{Message: "broken stream"}, "llm: broken stream"},
		{"cause", &Error{Kind: ErrorTransport, Cause: cause}, "llm transport: dial failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.err.Error(); got != test.want {
				t.Fatalf("Error() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestErrorUnwrap 检查底层错误链。
func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("dial failed")
	wrapped := &Error{Kind: ErrorTransport, Cause: cause}

	if !errors.Is(wrapped, cause) {
		t.Fatal("Unwrap lost the cause")
	}
	if (&Error{}).Unwrap() != nil {
		t.Fatal("Unwrap returned a cause for an error without one")
	}
	if (*Error)(nil).Unwrap() != nil {
		t.Fatal("Unwrap must preserve nil")
	}
}

// TestErrorValidate 检查数值范围；Kind 词表有意不锁定。
func TestErrorValidate(t *testing.T) {
	if (*Error)(nil).Validate() == nil {
		t.Fatal("accepted nil error")
	}
	if err := (&Error{}).Validate(); err != nil {
		t.Fatalf("zero value rejected: %v", err)
	}
	if err := (&Error{Kind: "vendor_specific"}).Validate(); err != nil {
		t.Fatalf("extension kind rejected: %v", err)
	}

	invalid := []*Error{
		{StatusCode: -1},
		{RetryAfterSeconds: -1},
	}
	for index, err := range invalid {
		if err.Validate() == nil {
			t.Errorf("accepted invalid error %d", index)
		}
	}
}
