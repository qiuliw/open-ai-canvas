package app

import (
	"encoding/json"
	"errors"
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
	if !isContentModerationFailure(`InputTextSensitiveContentDetected`) {
		t.Fatal("expected InputTextSensitiveContentDetected to be moderation")
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
	failed, failure := interpretUpstream(nil, http.StatusOK, body)
	if !failed || failure.Code != "RequestParameterIsWrong" || failure.Message != "参数: prompt 的长度: 23142 大于最大长度 10000" {
		t.Fatalf("business = %#v failed=%v", failure, failed)
	}
	if failure.Kind != providerFailureBusiness {
		t.Fatalf("Kind = %q, want business", failure.Kind)
	}
	if failure.StatusCode != http.StatusOK {
		t.Fatalf("2xx business failure should keep StatusCode = %d, got %d", http.StatusOK, failure.StatusCode)
	}
	if got := failure.Error(); strings.Contains(got, "HTTP 200") {
		t.Fatalf("business failure must not surface HTTP 200: %q", got)
	}
}

func TestInterpretUpstreamAcceptsStringSuccessCode(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"code": "Success", "data": map[string]any{"task_id": "task-1"}})
	if failed, failure := interpretUpstream(nil, http.StatusOK, body); failed {
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
	if failed, failure := interpretUpstream(nil, http.StatusOK, body); failed {
		t.Fatalf("vendor nested output must not be host-special-cased: %#v", failure)
	}
}

func TestAttachUpstreamTaskIDEnablesPendingWithoutReinterpret(t *testing.T) {
	raw := providerFailure{
		Kind:           providerFailureHTTP,
		StatusCode:     http.StatusBadRequest,
		Body:           `{"code":"task_not_exist"}`,
		Code:           "task_not_exist",
		ChannelPending: true,
	}
	err := attachUpstreamTaskID(raw, "task-1")
	var out providerFailure
	if !errors.As(err, &out) || !out.Pending || out.TaskID != "task-1" || out.Code != "task_not_exist" {
		t.Fatalf("attach = %#v", out)
	}
	if err := attachUpstreamTaskID(raw, ""); true {
		var still providerFailure
		if !errors.As(err, &still) || still.Pending {
			t.Fatalf("empty taskID must not enable Pending: %#v", still)
		}
	}
}

func TestChannelBoundInterpretMarksPendingOnce(t *testing.T) {
	adapter := newDeclarativeNewAPIChannel2TestAdapter(t)
	body := []byte(`{"code":"task_not_exist","message":"task_not_exist"}`)
	failed, failure := interpretUpstream(adapter, http.StatusBadRequest, body)
	if !failed || !failure.ChannelPending || failure.Pending {
		t.Fatalf("interpret = %#v, want ChannelPending without Pending", failure)
	}
	err := attachUpstreamTaskID(failure, "task-9")
	var out providerFailure
	if !errors.As(err, &out) || !out.ChannelPending || !out.Pending || out.TaskID != "task-9" {
		t.Fatalf("attach = %#v", out)
	}
	// Second attach must not clear Pending or re-parse.
	err = attachUpstreamTaskID(out, "task-9")
	if !errors.As(err, &out) || out.TaskID != "task-9" || !out.Pending {
		t.Fatalf("second attach = %#v", out)
	}
}

func TestProviderFailureCategoryTextSensitiveIsSafetyNotPortrait(t *testing.T) {
	raw := `{"error":{"code":"InputTextSensitiveContentDetected","message":"The request failed because the input text may contain sensitive information"}}`
	message, ok := providerFailureCategory(raw)
	if !ok || !strings.Contains(message, "安全审核") {
		t.Fatalf("text sensitive = %q ok=%v, want 安全审核", message, ok)
	}
	if strings.Contains(message, "真人形象") {
		t.Fatalf("text sensitive must not map to portrait: %q", message)
	}
}
