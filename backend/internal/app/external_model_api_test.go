package app

import (
	"encoding/json"
	"testing"
	"yingce/backend/internal/model"
)

func TestExternalModelSystemInstructionsReachCanonicalProtocol(t *testing.T) {
	taskReq, err := modelCallTaskRequest(CreateModelCallRequest{Model: "channel::model", Messages: []ModelCallMessage{{Role: "system", Content: "Be concise"}, {Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(taskReq.Input)
	if err != nil {
		t.Fatal(err)
	}
	var input canvasGenerationInput
	if err := json.Unmarshal(encoded, &input); err != nil {
		t.Fatal(err)
	}
	request := protocolRequestFromInput(input)
	if request.Instructions != "Be concise" || request.Prompt != "hello" {
		t.Fatalf("instructions lost: %+v", request)
	}
	for _, m := range request.Messages {
		if m.Role == "system" {
			t.Fatal("system duplicated into history")
		}
	}
}

func TestExternalModelOutputLimitMatchesBillingAndModelCeiling(t *testing.T) {
	profile := DefaultModelCapabilityConfigForModel("chat-completion", "demo")
	profile.Text.MaxOutputTokens = 512
	s, _, channel, cm := createSystemChannelSelectionFixture(t, "text", model.ChannelInterfaceType("chat-completion"), profile, nil)
	for _, tc := range []struct{ requested, want int }{{64, 64}, {1024, 512}, {0, 512}} {
		req, err := modelCallTaskRequest(CreateModelCallRequest{Model: channel.ID + "::" + cm.ModelKey, Prompt: "hello", Parameters: ModelCallParameters{MaxOutputTokens: tc.requested}})
		if err != nil {
			t.Fatal(err)
		}
		input, err := normalizeTaskInput(req.Input)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.normalizeModelCallTextLimit(input); err != nil {
			t.Fatal(err)
		}
		if got := estimateTaskTokens(input).OutputTokens; got != int64(tc.want) {
			t.Fatalf("billing output=%d want %d", got, tc.want)
		}
		encoded, _ := json.Marshal(input)
		var generation canvasGenerationInput
		if err := json.Unmarshal(encoded, &generation); err != nil {
			t.Fatal(err)
		}
		if generation.TextOptions.MaxOutputTokens != tc.want {
			t.Fatalf("worker cap=%d want %d", generation.TextOptions.MaxOutputTokens, tc.want)
		}
	}
}

func TestExternalClaudeThinkingRejectsInsufficientOutputBudget(t *testing.T) {
	profile := DefaultModelCapabilityConfigForModel("claude-api", "claude")
	s, _, channel, cm := createSystemChannelSelectionFixture(t, "text", model.ChannelInterfaceClaudeAPI, profile, nil)
	for _, tc := range []struct {
		limit     int
		wantError bool
	}{{512, true}, {1024, true}, {2048, false}} {
		req, err := modelCallTaskRequest(CreateModelCallRequest{Model: channel.ID + "::" + cm.ModelKey, Prompt: "hello", Parameters: ModelCallParameters{MaxOutputTokens: tc.limit, Thinking: true}})
		if err != nil {
			t.Fatal(err)
		}
		input, err := normalizeTaskInput(req.Input)
		if err != nil {
			t.Fatal(err)
		}
		err = s.normalizeModelCallTextLimit(input)
		if (err != nil) != tc.wantError {
			t.Fatalf("limit=%d err=%v", tc.limit, err)
		}
	}
}
