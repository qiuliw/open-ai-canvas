package protocol

import (
	"context"
	"testing"
)

func TestDashscopeMinimaxVideoCreateAndLifecycle(t *testing.T) {
	adapter := officialPackageAdapter(t, "dashscope-minimax-video.yingce-plugin", "dashscope-minimax-video")
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "MiniMax/MiniMax-H3", Prompt: "一只猫在海边奔跑", Duration: 6, AspectRatio: "16:9", Watermark: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if create.Method != "POST" || create.Path != "/api/v1/services/aigc/video-generation/video-synthesis" || !create.OriginPath || create.Headers["X-DashScope-Async"] != "enable" {
		t.Fatalf("create = %#v", create)
	}
	body := manifestTestBody(t, create)
	input, _ := body["input"].(map[string]any)
	if input["prompt"] != "一只猫在海边奔跑" {
		t.Fatalf("input = %#v", input)
	}
	parameters, _ := body["parameters"].(map[string]any)
	if parameters["resolution"] != "768P" || parameters["ratio"] != "16:9" || parameters["duration"] != float64(6) || parameters["watermark"] != true {
		t.Fatalf("parameters = %#v", parameters)
	}

	references, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "MiniMax/MiniMax-H3", Prompt: "多模态参考", Duration: 6,
		Inputs: []MediaReference{
			{URL: "https://cdn.example/audio.mp3", Kind: "audio", Role: "reference_audio", Order: 3},
			{URL: "https://cdn.example/image.png", Kind: "image", Role: "reference_image", Order: 1},
			{URL: "https://cdn.example/video.mp4", Kind: "video", Role: "reference_video", Order: 2},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	refMedia, _ := manifestTestBody(t, references)["input"].(map[string]any)["media"].([]any)
	if len(refMedia) != 3 || refMedia[0].(map[string]any)["type"] != "image_url" || refMedia[1].(map[string]any)["type"] != "feature" || refMedia[2].(map[string]any)["type"] != "driving_audio" {
		t.Fatalf("media = %#v", refMedia)
	}

	keyframes, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "MiniMax/MiniMax-H3", Prompt: "首尾帧", Duration: 6,
		Images: []MediaReference{
			{URL: "https://cdn.example/last.png", Role: "last_frame", Order: 2},
			{URL: "https://cdn.example/first.png", Role: "first_frame", Order: 1},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	keyBody := manifestTestBody(t, keyframes)
	keyMedia, _ := keyBody["input"].(map[string]any)["media"].([]any)
	keyParameters, _ := keyBody["parameters"].(map[string]any)
	if len(keyMedia) != 2 || keyMedia[0].(map[string]any)["type"] != "first_frame" || keyMedia[1].(map[string]any)["type"] != "last_frame" || keyParameters["ratio"] != "adaptive" {
		t.Fatalf("keyframes = %#v parameters = %#v", keyMedia, keyParameters)
	}

	if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "wan3.0-video-prime", Prompt: "x", Duration: 6}}); err == nil {
		t.Fatal("unsupported model accepted")
	}
	if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "MiniMax/MiniMax-H3", Prompt: "互斥", Duration: 6,
		Images: []MediaReference{
			{URL: "https://cdn.example/first.png", Role: "first_frame", Order: 1},
			{URL: "https://cdn.example/ref.png", Kind: "image", Role: "reference_image", Order: 2},
		},
	}}); err == nil {
		t.Fatal("mixed keyframe and reference modes accepted")
	}

	created, err := adapter.ParseCreate(context.Background(), []byte(`{"output":{"task_id":"minimax-task-1","task_status":"PENDING"}}`))
	if err != nil || created.TaskID != "minimax-task-1" || created.Status != StatusPending {
		t.Fatalf("create result = %#v err=%v", created, err)
	}
	poll, err := adapter.BuildPoll(context.Background(), PollContext{TaskID: created.TaskID})
	if err != nil || poll.Path != "/api/v1/tasks/minimax-task-1" || !poll.OriginPath {
		t.Fatalf("poll = %#v err=%v", poll, err)
	}
	succeeded, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: created.TaskID}, []byte(`{"output":{"task_id":"minimax-task-1","task_status":"SUCCEEDED","video_url":"https://maas-result.example/minimax.mp4"},"usage":{"video_count":1}}`))
	if err != nil || succeeded.Status != StatusSucceeded || len(succeeded.Result.Videos) != 1 || !succeeded.Result.Videos[0].Ephemeral {
		t.Fatalf("success = %#v err=%v", succeeded, err)
	}
	unknown, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: created.TaskID}, []byte(`{"output":{"task_id":"minimax-task-1","task_status":"UNKNOWN"}}`))
	if err != nil || unknown.Status != StatusFailed {
		t.Fatalf("unknown = %#v err=%v", unknown, err)
	}
}
