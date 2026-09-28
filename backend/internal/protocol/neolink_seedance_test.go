package protocol

import (
	"context"
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
