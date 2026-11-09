package app

import (
	"encoding/json"
	"testing"
	"yingce/backend/internal/model"
	"yingce/backend/internal/protocol"
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
	for _, budget := range []int{0, 4096} {
		profile := DefaultModelCapabilityConfigForModel("claude-api", "claude")
		profile.Text.MaxOutputTokens = 8192
		if budget > 0 {
			profile.Text.Thinking = &TextThinkingConfig{
				Mode: ThinkingModeToggle, Levels: []string{ThinkingLevelOff, ThinkingLevelMedium}, Default: ThinkingLevelOff,
				Switch:    ThinkingSwitchConfig{Kind: ThinkingSwitchThinkingType},
				Intensity: ThinkingIntensityConfig{Kind: ThinkingIntensityBudget, Budgets: map[string]int{ThinkingLevelMedium: budget}},
			}
		}
		s, _, channel, cm := createSystemChannelSelectionFixture(t, "text", model.ChannelInterfaceClaudeAPI, profile, nil)
		for _, limit := range []int{512, 1024, 2048, 8192} {
			for _, thinking := range []bool{false, true} {
				req, err := modelCallTaskRequest(CreateModelCallRequest{Model: channel.ID + "::" + cm.ModelKey, Prompt: "hello", Parameters: ModelCallParameters{MaxOutputTokens: limit, Thinking: thinking}})
				if err != nil {
					t.Fatal(err)
				}
				input, err := normalizeTaskInput(req.Input)
				if err != nil {
					t.Fatal(err)
				}
				err = s.normalizeModelCallTextLimit(input)
				wantError := thinking && limit <= max(budget, thinkingBudgetFloor)
				if (err != nil) != wantError {
					t.Fatalf("budget=%d limit=%d thinking=%v err=%v", budget, limit, thinking, err)
				}
				if err != nil {
					continue
				}
				encoded, _ := json.Marshal(input)
				var generation canvasGenerationInput
				if err := json.Unmarshal(encoded, &generation); err != nil {
					t.Fatal(err)
				}
				generation.Config.CapabilityConfig = profile
				generation.MaxOutputTokens = limit
				body := map[string]interface{}{"max_tokens": limit}
				if got := applyTextThinking(body, generation, protocol.TextWireClaude); got != thinking {
					t.Fatalf("thinking enabled=%v want %v", got, thinking)
				}
				if got := intFromAny(body["max_tokens"]); got != limit {
					t.Fatalf("execution exceeded admitted limit: got=%d want=%d", got, limit)
				}
			}
		}
	}
}
