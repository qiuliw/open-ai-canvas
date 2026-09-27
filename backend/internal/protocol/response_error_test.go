package protocol

import (
	"net/http"
	"testing"
)

func TestInterpretBusinessFailure(t *testing.T) {
	adapter := officialPackageAdapter(t, "dashscope-wan3-video.yingce-plugin", "dashscope-wan3-video")
	failed, handled := Interpret(adapter, http.StatusOK, []byte(`{"output":{"code":"InvalidParameter","message":"bad resolution"}}`))
	if !handled || !failed.Failed || failed.Code != "InvalidParameter" || failed.Message != "bad resolution" {
		t.Fatalf("business = %#v handled=%v", failed, handled)
	}
	okBody, handled := Interpret(adapter, http.StatusOK, []byte(`{"output":{"task_status":"PENDING"}}`))
	if !handled || okBody.Failed {
		t.Fatalf("pending output must not be a business failure: %#v", okBody)
	}
}

func TestInterpretNon2xxPendingAndMapping(t *testing.T) {
	adapter, err := LoadManifest([]byte(`{
		"apiVersion":"yingce.plugin/v1",
		"id":"http-error-test","version":"1.0.0","name":"HTTP Error Test","author":"Test","documentation":"# Test",
		"contributes":{"providers":[{"id":"http-error-test","label":"HTTP Error Test","capabilities":["video"],"scopes":["canvas"],
			"create":{"method":"POST","path":"/tasks"},
			"response":{
				"errorPaths":["code"],
				"messagePaths":["message"],
				"statusCodeMapping":{"429":503},
				"httpErrors":[{"statusCodes":[400],"equals":["task_not_exist"],"status":"pending"}]
			}}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !IsManifestAdapter(adapter) {
		t.Fatal("expected manifest adapter")
	}

	pending, handled := Interpret(adapter, http.StatusBadRequest, []byte(`{"code":"task_not_exist","message":"task_not_exist"}`))
	if !handled || !pending.Failed || !pending.Pending || pending.Code != "task_not_exist" {
		t.Fatalf("pending = %#v handled=%v", pending, handled)
	}

	failed, handled := Interpret(adapter, http.StatusBadRequest, []byte(`{"code":"invalid_parameter","message":"bad size"}`))
	if !handled || !failed.Failed || failed.Pending || failed.Message != "bad size" || failed.MappedStatusCode != 0 {
		t.Fatalf("failed = %#v handled=%v", failed, handled)
	}

	mapped, handled := Interpret(adapter, http.StatusTooManyRequests, []byte(`{"code":"rate_limit","message":"slow down"}`))
	if !handled || !mapped.Failed || mapped.MappedStatusCode != http.StatusServiceUnavailable {
		t.Fatalf("mapped = %#v handled=%v", mapped, handled)
	}
}
