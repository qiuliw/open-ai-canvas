package protocol

import (
	"context"
	"strings"
	"testing"
)

func TestNeolinkSeedanceCreatePollAndCancelPaths(t *testing.T) {
	adapter := officialPackageAdapter(t, "neolink-seedance.yingce-plugin", "neolink-seedance")
	meta := adapter.Metadata()
	if meta.ID != "neolink-seedance" || !meta.RequiresPublicMediaURLs {
		t.Fatalf("metadata = %#v", meta)
	}

	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "doubao-seedance-2-0-mini-260615", Prompt: "一只橘猫在窗台晒太阳", Duration: 5, AspectRatio: "16:9", Resolution: "720p",
		GenerateAudio: true,
		ProviderOptions: map[string]map[string]any{"neolink-seedance": {"seed": 7}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if create.Method != "POST" || create.Path != "/contents/generations/tasks" || create.OriginPath {
		t.Fatalf("create = %#v", create)
	}
	body := manifestTestBody(t, create)
	if body["model"] != "doubao-seedance-2-0-mini-260615" || body["ratio"] != "16:9" || body["resolution"] != "720p" || body["duration"] != float64(5) || body["generate_audio"] != true || body["seed"] != float64(7) {
		t.Fatalf("body = %#v", body)
	}
	content, _ := body["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %#v", content)
	}

	poll, err := adapter.BuildPoll(context.Background(), PollContext{TaskID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if poll.Method != "GET" || poll.Path != "/contents/generations/tasks/task-1" {
		t.Fatalf("poll = %#v", poll)
	}

	cancel, err := adapter.BuildCancel(context.Background(), PollContext{TaskID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if cancel.Method != "DELETE" || cancel.Path != "/contents/generations/tasks/task-1" {
		t.Fatalf("cancel = %#v", cancel)
	}
}

func TestNeolinkSeedanceMapsSensitiveErrorCodesToChinese(t *testing.T) {
	adapter := officialPackageAdapter(t, "neolink-seedance.yingce-plugin", "neolink-seedance")
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "text sensitive",
			body: `{"id":"task-1","status":"failed","error":{"code":"InputTextSensitiveContentDetected","message":"The request failed because the input text may contain sensitive information. Request id: secret"}}`,
			want: "提示词未通过内容安全审核，请修改后重试",
		},
		{
			name: "real person image",
			body: `{"id":"task-1","status":"failed","error":{"code":"InputImageSensitiveContentDetected.PrivacyInformation","message":"may contain real person. Request id: secret"}}`,
			want: "输入图片疑似包含真人形象，请更换素材或改用其他模型",
		},
		{
			name: "unknown code keeps upstream message",
			body: `{"id":"task-1","status":"failed","error":{"code":"SomeOtherCode","message":"自定义上游失败原因"}}`,
			want: "自定义上游失败原因",
		},
		{
			name: "gateway wraps ark error json in message string",
			body: `{"code":"fail_to_fetch_task","data":null,"message":"{\"error\":{\"code\":\"InputTextSensitiveContentDetected\",\"message\":\"The request failed because the input text 'content[0]' may contain sensitive information. Request id: secret\",\"type\":\"BadRequest\"}}"}`,
			want: "提示词未通过内容安全审核，请修改后重试",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-1"}, []byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusFailed {
				t.Fatalf("status = %q, want failed", result.Status)
			}
			if result.Message != tt.want {
				t.Fatalf("message = %q, want %q", result.Message, tt.want)
			}
			if strings.Contains(result.Message, "Request id") || strings.Contains(result.Message, "secret") {
				t.Fatalf("message leaked diagnostics: %q", result.Message)
			}
		})
	}
}
