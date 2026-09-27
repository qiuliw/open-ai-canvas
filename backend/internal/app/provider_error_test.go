package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestProviderFailureDetailsReadsTopLevelModerationError(t *testing.T) {
	code, message := generalErrorFields(map[string]any{
		"code":    contentModerationErrorCode,
		"message": "prompt rejected",
	})
	if code != contentModerationErrorCode {
		t.Fatalf("unexpected code: %q", code)
	}
	if message != "prompt rejected" {
		t.Fatalf("unexpected message: %q", message)
	}
}

func TestProviderFailureDetailsReadsNestedError(t *testing.T) {
	code, message := generalErrorFields(map[string]any{
		"error": map[string]any{"code": "invalid_request", "message": "invalid size"},
	})
	if code != "invalid_request" || message != "invalid size" {
		t.Fatalf("unexpected failure details: code=%q message=%q", code, message)
	}
}

func TestProviderFailureDetailsIgnoresVendorDataNesting(t *testing.T) {
	// new-api GeneralErrorResponse has no data.*; vendor nesting belongs in plugin paths.
	code, message := generalErrorFields(map[string]any{
		"code": float64(400),
		"data": map[string]any{"code": contentModerationErrorCode, "message": "prompt rejected"},
	})
	if code != "400" || message != "" {
		t.Fatalf("unexpected wrapped failure details: code=%q message=%q", code, message)
	}
}

func TestContentModerationFailureRequiresExactProviderCode(t *testing.T) {
	if !isContentModerationFailure(`{"code":"sensitive_words_detected"}`) {
		t.Fatal("expected moderation error to be detected")
	}
	if isContentModerationFailure("上游 HTTP 400") {
		t.Fatal("generic HTTP 400 must remain retryable")
	}
}

func TestInterpretUpstreamBusinessEnvelope(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"code": "RequestParameterIsWrong",
		"data": nil,
		"msg":  "参数: prompt 的长度: 23142 大于最大长度 10000",
	})
	failed, failure := interpretUpstream(nil, "", http.StatusOK, body)
	if !failed || failure.Code != "RequestParameterIsWrong" || failure.Message != "参数: prompt 的长度: 23142 大于最大长度 10000" {
		t.Fatalf("business = %#v failed=%v", failure, failed)
	}
	if failure.StatusCode != 0 {
		t.Fatalf("2xx business failure StatusCode = %d, want 0", failure.StatusCode)
	}
	if got := failure.Error(); strings.Contains(got, "HTTP 200") {
		t.Fatalf("business failure must not surface HTTP 200: %q", got)
	}
}

func TestInterpretUpstreamAcceptsStringSuccessCode(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"code": "Success", "data": map[string]any{"task_id": "task-1"}})
	if failed, failure := interpretUpstream(nil, "", http.StatusOK, body); failed {
		t.Fatalf("success payload was marked failed: %#v", failure)
	}
}

func TestInterpretUpstreamIgnoresVendorNestedOutput(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"output": map[string]any{
			"code":        "InvalidParameter",
			"message":     "Input should be '1080P', '720P' or '480P': parameters.resolution",
			"task_status": "FAILED",
		},
	})
	if failed, failure := interpretUpstream(nil, "", http.StatusOK, body); failed {
		t.Fatalf("vendor nested output must not be host-special-cased: %#v", failure)
	}
}
