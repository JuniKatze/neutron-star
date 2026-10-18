package chatcompletions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"neutron-star/internal/llm"
)

// httpTestResponse 构造一个带给定状态码、头和响应体的响应。
func httpTestResponse(
	status int,
	header map[string]string,
	body string,
) *http.Response {
	response := &http.Response{
		StatusCode: status,
		Header:     make(http.Header, len(header)),
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	for name, value := range header {
		response.Header.Set(name, value)
	}

	return response
}

func TestHTTPRequestHeaders(t *testing.T) {
	client, err := optionsTestNew(t, Options{
		APIKey:  "secret",
		Headers: map[string]string{"X-Extra": "yes"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	request, err := client.newRequest(
		context.Background(),
		[]byte(`{"model":"m"}`),
	)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}

	if request.Method != http.MethodPost {
		t.Fatalf("method: %q", request.Method)
	}
	if request.URL.String() != optionsTestEndpoint {
		t.Fatalf("url: %q", request.URL)
	}
	if request.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type: %q", request.Header.Get("Content-Type"))
	}
	if request.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("accept: %q", request.Header.Get("Accept"))
	}
	if request.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("authorization: %q", request.Header.Get("Authorization"))
	}
	if request.Header.Get("X-Extra") != "yes" {
		t.Fatalf("extra header: %q", request.Header.Get("X-Extra"))
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != `{"model":"m"}` {
		t.Fatalf("body: %q", body)
	}
}

func TestHTTPRequestHeadersCanOverrideDefaults(t *testing.T) {
	client, err := optionsTestNew(t, Options{
		APIKey: "secret",
		Headers: map[string]string{
			"Authorization": "Basic other",
			"Accept":        "application/json",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	request, err := client.newRequest(context.Background(), nil)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}

	if request.Header.Get("Authorization") != "Basic other" {
		t.Fatalf("authorization: %q", request.Header.Get("Authorization"))
	}
	if request.Header.Get("Accept") != "application/json" {
		t.Fatalf("accept: %q", request.Header.Get("Accept"))
	}
}

func TestHTTPRequestSendsNoAuthorizationWithoutKey(t *testing.T) {
	client, err := optionsTestNew(t, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	request, err := client.newRequest(context.Background(), nil)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}

	if value := request.Header.Get("Authorization"); value != "" {
		t.Fatalf("authorization: %q", value)
	}
}

func TestHTTPStatusClassification(t *testing.T) {
	cases := []struct {
		status int
		want   llm.ErrorKind
	}{
		{http.StatusBadRequest, llm.ErrorInvalidRequest},
		{http.StatusUnauthorized, llm.ErrorAuthentication},
		{http.StatusForbidden, llm.ErrorPermissionDenied},
		{http.StatusNotFound, llm.ErrorNotFound},
		{http.StatusMethodNotAllowed, llm.ErrorInvalidRequest},
		{http.StatusRequestTimeout, llm.ErrorTimeout},
		{http.StatusConflict, llm.ErrorInvalidRequest},
		{http.StatusRequestEntityTooLarge, llm.ErrorInvalidRequest},
		{http.StatusUnsupportedMediaType, llm.ErrorInvalidRequest},
		{http.StatusUnprocessableEntity, llm.ErrorInvalidRequest},
		{http.StatusTooManyRequests, llm.ErrorRateLimited},
		{http.StatusInternalServerError, llm.ErrorUnavailable},
		{http.StatusBadGateway, llm.ErrorUnavailable},
		{http.StatusServiceUnavailable, llm.ErrorUnavailable},
		{http.StatusGatewayTimeout, llm.ErrorTimeout},
		{http.StatusTeapot, llm.ErrorInvalidRequest},
	}

	for _, testCase := range cases {
		got := kindFromStatus(testCase.status)
		if got != testCase.want {
			t.Errorf(
				"status %d: got %q, want %q",
				testCase.status,
				got,
				testCase.want,
			)
		}
	}
}

func TestHTTPErrorFromResponseEnvelope(t *testing.T) {
	response := httpTestResponse(
		http.StatusBadRequest,
		nil,
		`{"error":{"message":"bad model","type":"invalid_request_error",`+
			`"code":"model_not_found","param":"model"}}`,
	)

	err := errorFromResponse(response, "req-9")

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorInvalidRequest {
		t.Fatalf("kind: %q", failure.Kind)
	}
	if failure.Message != "bad model" {
		t.Fatalf("message: %q", failure.Message)
	}
	if failure.ProviderCode != "model_not_found" {
		t.Fatalf("provider code: %q", failure.ProviderCode)
	}
	if failure.RequestID != "req-9" {
		t.Fatalf("request id: %q", failure.RequestID)
	}
	if failure.StatusCode != http.StatusBadRequest {
		t.Fatalf("status code: %d", failure.StatusCode)
	}
}

func TestHTTPErrorFromResponseNumericCode(t *testing.T) {
	// 有的实现把 code 写成数字，原文必须保留。
	response := httpTestResponse(
		http.StatusTooManyRequests,
		nil,
		`{"error":{"message":"slow down","code":429}}`,
	)

	err := errorFromResponse(response, "")

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.ProviderCode != "429" {
		t.Fatalf("provider code: %q", failure.ProviderCode)
	}
}

func TestHTTPErrorFromResponsePlainText(t *testing.T) {
	response := httpTestResponse(
		http.StatusBadGateway,
		nil,
		"upstream says no",
	)

	err := errorFromResponse(response, "")

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Message != "upstream says no" {
		t.Fatalf("message: %q", failure.Message)
	}
	if failure.Kind != llm.ErrorUnavailable {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestHTTPErrorFromResponseEmptyBody(t *testing.T) {
	response := httpTestResponse(http.StatusBadGateway, nil, "")

	err := errorFromResponse(response, "")

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Message != "Bad Gateway" {
		t.Fatalf("message: %q", failure.Message)
	}
}

func TestHTTPErrorFromChunkClassification(t *testing.T) {
	cases := []struct {
		name string
		body string
		want llm.ErrorKind
	}{
		{
			"rate limit",
			`{"type":"rate_limit_error","code":null}`,
			llm.ErrorRateLimited,
		},
		{
			"quota",
			`{"type":"insufficient_quota","code":"insufficient_quota"}`,
			llm.ErrorQuota,
		},
		{
			"authentication",
			`{"type":"authentication_error","code":null}`,
			llm.ErrorAuthentication,
		},
		{
			"api key",
			`{"type":"invalid_request_error","code":"invalid_api_key"}`,
			llm.ErrorAuthentication,
		},
		{
			"permission",
			`{"type":"permission_error","code":null}`,
			llm.ErrorPermissionDenied,
		},
		{
			"not found",
			`{"type":"not_found_error","code":null}`,
			llm.ErrorNotFound,
		},
		{
			"invalid",
			`{"type":"invalid_request_error","code":null}`,
			llm.ErrorInvalidRequest,
		},
		{
			"context length",
			`{"type":null,"code":"context_length_exceeded"}`,
			llm.ErrorInvalidRequest,
		},
		{
			"overloaded",
			`{"type":"overloaded_error","code":null}`,
			llm.ErrorUnavailable,
		},
		{
			"server error",
			`{"type":"server_error","code":null}`,
			llm.ErrorUnavailable,
		},
		{
			"unknown",
			`{"type":"something_new","code":null}`,
			llm.ErrorUnavailable,
		},
	}

	for _, testCase := range cases {
		var value wireError
		if err := json.Unmarshal([]byte(testCase.body), &value); err != nil {
			t.Fatalf("%s: decode: %v", testCase.name, err)
		}

		err := errorFromChunk(&value, "req-1")

		var failure *llm.Error
		if !errors.As(err, &failure) {
			t.Fatalf("%s: error type: %v", testCase.name, err)
		}
		if failure.Kind != testCase.want {
			t.Errorf(
				"%s: got %q, want %q",
				testCase.name,
				failure.Kind,
				testCase.want,
			)
		}
		if failure.StatusCode != http.StatusOK {
			t.Errorf(
				"%s: status code: %d",
				testCase.name,
				failure.StatusCode,
			)
		}
		if failure.RequestID != "req-1" {
			t.Errorf("%s: request id: %q", testCase.name, failure.RequestID)
		}
	}
}

func TestHTTPErrorFromChunkDefaultMessage(t *testing.T) {
	err := errorFromChunk(&wireError{}, "")

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Message != "provider reported an error in the stream" {
		t.Fatalf("message: %q", failure.Message)
	}
}

func TestHTTPParseRetryAfter(t *testing.T) {
	past := time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)
	future := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)

	cases := []struct {
		name       string
		value      string
		wantSecond int64
		wantOK     bool
	}{
		{"empty", "", 0, false},
		{"seconds", "3", 3, true},
		{"zero", "0", 0, false},
		{"negative", "-2", 0, false},
		{"garbage", "soon", 0, false},
		{"past date", past, 0, false},
	}

	for _, testCase := range cases {
		got, ok := parseRetryAfter(testCase.value)
		if ok != testCase.wantOK || got != testCase.wantSecond {
			t.Errorf(
				"%s: got (%d, %v), want (%d, %v)",
				testCase.name,
				got,
				ok,
				testCase.wantSecond,
				testCase.wantOK,
			)
		}
	}

	got, ok := parseRetryAfter(future)
	if !ok {
		t.Fatal("future date must be accepted")
	}
	if got < 1 || got > 6 {
		t.Fatalf("future date: got %d seconds", got)
	}
}

func TestHTTPCodeText(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"missing", "", ""},
		{"null", `null`, ""},
		{"string", `"insufficient_quota"`, "insufficient_quota"},
		{"number", `429`, "429"},
		{"object", `{"a":1}`, `{"a":1}`},
	}

	for _, testCase := range cases {
		got := codeText([]byte(testCase.value))
		if got != testCase.want {
			t.Errorf(
				"%s: got %q, want %q",
				testCase.name,
				got,
				testCase.want,
			)
		}
	}
}

func TestHTTPClassifyTransportError(t *testing.T) {
	canceled := errors.New("connection reset")

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	deadlineCtx, cancelDeadline := context.WithDeadline(
		context.Background(),
		time.Now().Add(-time.Second),
	)
	defer cancelDeadline()

	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want llm.ErrorKind
	}{
		{
			"transport",
			context.Background(),
			errors.New("connection refused"),
			llm.ErrorTransport,
		},
		{
			"canceled context",
			canceledCtx,
			canceled,
			llm.ErrorCanceled,
		},
		{
			"deadline",
			deadlineCtx,
			errors.New("i/o timeout"),
			llm.ErrorTimeout,
		},
		{
			"canceled error",
			context.Background(),
			context.Canceled,
			llm.ErrorCanceled,
		},
		{
			"deadline error",
			context.Background(),
			context.DeadlineExceeded,
			llm.ErrorTimeout,
		},
	}

	for _, testCase := range cases {
		err := classifyTransportError(testCase.ctx, testCase.err)

		var failure *llm.Error
		if !errors.As(err, &failure) {
			t.Fatalf("%s: error type: %v", testCase.name, err)
		}
		if failure.Kind != testCase.want {
			t.Errorf(
				"%s: got %q, want %q",
				testCase.name,
				failure.Kind,
				testCase.want,
			)
		}
	}
}

func TestHTTPClassifyTransportErrorKeepsCause(t *testing.T) {
	cause := errors.New("connection refused")

	err := classifyTransportError(context.Background(), cause)

	if !errors.Is(err, cause) {
		t.Fatalf("cause is not preserved: %v", err)
	}
}

func TestHTTPClassifyContextErrorPassesOtherErrors(t *testing.T) {
	other := errors.New("something else")

	if got := classifyContextError(other); got != other {
		t.Fatalf("unexpected error: %v", got)
	}
}

func TestHTTPReadErrorBodyLimit(t *testing.T) {
	body, err := readErrorBody(strings.NewReader("0123456789"), 4)
	if err != nil {
		t.Fatalf("readErrorBody: %v", err)
	}
	if string(body) != "0123" {
		t.Fatalf("body: %q", body)
	}
}
