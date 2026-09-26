package protocol

import (
	"context"
	"net/http"
	"testing"
)

func TestManifestInterpretHTTPErrorPendingAndMapping(t *testing.T) {
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
	interpreter, ok := adapter.(HTTPErrorAdapter)
	if !ok {
		t.Fatal("manifest adapter must implement HTTPErrorAdapter")
	}

	pending, handled := interpreter.InterpretHTTPError(context.Background(), http.StatusBadRequest, []byte(`{"code":"task_not_exist","message":"task_not_exist"}`))
	if !handled || pending.Status != StatusPending || pending.Code != "task_not_exist" {
		t.Fatalf("pending = %#v handled=%v", pending, handled)
	}

	failed, handled := interpreter.InterpretHTTPError(context.Background(), http.StatusBadRequest, []byte(`{"code":"invalid_parameter","message":"bad size"}`))
	if !handled || failed.Status != StatusFailed || failed.Message != "bad size" || failed.MappedStatusCode != 0 {
		t.Fatalf("failed = %#v handled=%v", failed, handled)
	}

	mapped, handled := interpreter.InterpretHTTPError(context.Background(), http.StatusTooManyRequests, []byte(`{"code":"rate_limit","message":"slow down"}`))
	if !handled || mapped.Status != StatusFailed || mapped.MappedStatusCode != http.StatusServiceUnavailable {
		t.Fatalf("mapped = %#v handled=%v", mapped, handled)
	}
}
