package protocol

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestBuiltinCatalogContainsRequestedProtocols(t *testing.T) {
	registry := Builtins()
	hosted := []string{
		"chat-completion", "openai-response", "claude-api",
		"newapi", "newapi-channel-1", "newapi-channel-2",
		"xai-video", "volcengine-ark-video", "volcengine-jimeng-video",
		"gemini-veo", "novita-video", "minimax-video", "agnes-video",
		"openai-image", "grok-image", "volcengine-ark-image", "volcengine-jimeng-image",
		"openai-audio", "async-audio",
	}
	for _, id := range hosted {
		if _, ok := registry.Get(id); ok {
			t.Fatalf("host builtin must not keep protocol %q", id)
		}
	}
	if _, ok := registry.Get("autodl-comfyui"); ok {
		t.Fatal("AutoDL must be provided by the uploaded plugin package, not the host registry")
	}
}

func TestTextAdaptersMapRequestsAndResponses(t *testing.T) {
	cases := []struct {
		packageName, id, path string
		body                  map[string]any
		resp                  string
		want                  string
	}{
		{"openai-chat-completions.yingce-plugin", "chat-completion", "/chat/completions", map[string]any{"model": "gpt-test", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}, `{"choices":[{"message":{"content":"chat answer"}}]}`, "chat answer"},
		{"openai-responses.yingce-plugin", "openai-response", "/responses", map[string]any{"model": "o-test", "input": "hello"}, `{"output_text":"response answer"}`, "response answer"},
		{"anthropic-messages.yingce-plugin", "claude-api", "/v1/messages", map[string]any{"model": "claude-test", "max_tokens": float64(4096)}, `{"content":[{"type":"text","text":"claude answer"}]}`, "claude answer"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			adapter := officialPackageAdapter(t, tc.packageName, tc.id)
			spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: tc.body["model"].(string), Prompt: "hello"}})
			if err != nil {
				t.Fatal(err)
			}
			if spec.Method != http.MethodPost || spec.Path != tc.path {
				t.Fatalf("request = %#v", spec)
			}
			body, _ := json.Marshal(spec.Body)
			var got map[string]any
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}
			for key, value := range tc.body {
				if key == "messages" {
					continue
				}
				if got[key] == nil {
					t.Fatalf("request missing %q: %#v", key, got)
				}
				_ = value
			}
			result, err := adapter.ParseCreate(context.Background(), []byte(tc.resp))
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusSucceeded || result.Result == nil || result.Result.Text != tc.want {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestClaudeAdapterDeclaresAnthropicVersion(t *testing.T) {
	adapter := officialPackageAdapter(t, "anthropic-messages.yingce-plugin", "claude-api")
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "claude-test", Prompt: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Headers["anthropic-version"] != "2023-06-01" {
		t.Fatalf("headers = %#v", spec.Headers)
	}
}

func TestImageAndVideoAdaptersMapProviderShapes(t *testing.T) {
	cases := []struct {
		packageName, id, path, poll string
		request                     GenerationRequest
	}{
		{"openai-videos.yingce-plugin", "newapi", "/v1/videos", "/v1/videos/video-1", GenerationRequest{Model: "video-model", Prompt: "a clip", Duration: 6}},
		{"newapi-video-generations-v1.yingce-plugin", "newapi-channel-2", "/v1/video/generations", "/v1/video/generations/video-1", GenerationRequest{Model: "video-model", Prompt: "a clip", Duration: 6, AspectRatio: "16:9"}},
		{"xai-video.yingce-plugin", "xai-video", "/v1/videos/generations", "/v1/videos/video-1", GenerationRequest{Model: "grok-video", Prompt: "a clip"}},
		{"volcengine-ark-seedance.yingce-plugin", "volcengine-ark-video", "/api/v3/contents/generations/tasks", "/api/v3/contents/generations/tasks/video-1", GenerationRequest{Model: "seedance", Prompt: "a clip"}},
		{"volcengine-jimeng-video.yingce-plugin", "volcengine-jimeng-video", "/", "/", GenerationRequest{Model: "jimeng_video", Prompt: "a clip"}},
		{"google-gemini-veo.yingce-plugin", "gemini-veo", "/v1beta/models/veo-test:predictLongRunning", "/v1beta/video-1", GenerationRequest{Model: "veo-test", Prompt: "a clip"}},
		{"novita-video.yingce-plugin", "novita-video", "/v3/video/create", "/v3/async/task-result", GenerationRequest{Model: "novita", Prompt: "a clip"}},
		{"minimax-hailuo-video-v2.yingce-plugin", "minimax-video", "/v2/video_generation", "/v2/query/video_generation/video-1", GenerationRequest{Model: "MiniMax-H3", Prompt: "a clip", Duration: 6}},
		{"agnes-video-25.yingce-plugin", "agnes-video", "/videos", "/agnesapi", GenerationRequest{Model: "agnes-video-2.5", Prompt: "a clip", Duration: 5}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			adapter := officialPackageAdapter(t, tc.packageName, tc.id)
			create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: tc.request})
			if err != nil {
				t.Fatal(err)
			}
			if create.Path != tc.path {
				t.Fatalf("create path = %q, want %q", create.Path, tc.path)
			}
			if tc.poll != "" {
				poll, err := adapter.BuildPoll(context.Background(), PollContext{Model: tc.request.Model, TaskID: "video-1"})
				if err != nil {
					t.Fatal(err)
				}
				if poll.Path != tc.poll {
					t.Fatalf("poll path = %q, want %q", poll.Path, tc.poll)
				}
			}
		})
	}
}

func TestArkVideoAdapterMapsFullModalReferences(t *testing.T) {
	adapter := officialPackageAdapter(t, "volcengine-ark-seedance.yingce-plugin", "volcengine-ark-video")
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "follow all references",
		Images: []MediaReference{{URL: "https://example.com/subject.png"}},
		Videos: []MediaReference{{URL: "https://example.com/motion.mp4"}},
		Audios: []MediaReference{{URL: "https://example.com/music.mp3"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body := spec.Body.(map[string]any)
	content := body["content"].([]any)
	want := []string{"text", "image_url", "video_url", "audio_url"}
	if len(content) != len(want) {
		t.Fatalf("content = %#v", content)
	}
	for index, item := range content {
		if item.(map[string]any)["type"] != want[index] {
			t.Fatalf("content[%d] = %#v, want type %q", index, item, want[index])
		}
	}

	_, err = adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "doubao-seedance-2-0-260128", Prompt: "follow the soundtrack", Audios: []MediaReference{{URL: "https://example.com/music.mp3"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "文本+音频") {
		t.Fatalf("audio-only combination error = %v", err)
	}
}

func TestVideoAdaptersTranslateImageRolesWithoutGuessingFromCount(t *testing.T) {
	tests := []struct {
		name, packageName, id, role string
	}{
		{name: "newapi channel 1", packageName: "newapi-media-task-v1.yingce-plugin", id: "newapi-channel-1", role: "reference_image"},
		{name: "volcengine ark", packageName: "volcengine-ark-seedance.yingce-plugin", id: "volcengine-ark-video", role: "first_frame"},
		{name: "minimax", packageName: "minimax-hailuo-video-v2.yingce-plugin", id: "minimax-video", role: "last_frame"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := officialPackageAdapter(t, test.packageName, test.id)
			images := []MediaReference{{ID: "image-1", URL: "https://cdn.example/image.png", Role: test.role}}
			if test.role == "last_frame" {
				images = append([]MediaReference{{URL: "https://cdn.example/first.png", Role: "first_frame"}}, images...)
			}
			spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
				Model: "video-model", Prompt: "test", Duration: 6,
				Images: images,
			}})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(spec.Body)
			if !strings.Contains(string(body), `"`+test.role+`"`) {
				t.Fatalf("body does not contain role %q: %s", test.role, body)
			}
		})
	}
}

func TestXAIVideoUsesReferenceImagesEvenForOneReferenceAsset(t *testing.T) {
	adapter := officialPackageAdapter(t, "xai-video.yingce-plugin", "xai-video")
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "grok-video", Prompt: "keep the character", Images: []MediaReference{{URL: "https://cdn.example/character.png", Role: "reference_image"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body := spec.Body.(map[string]any)
	if body["image"] != nil || body["reference_images"] == nil {
		t.Fatalf("xAI reference body = %#v", body)
	}
}

func TestStartFrameOnlyAdaptersRejectReferenceAssetSemantics(t *testing.T) {
	for _, tc := range []struct {
		packageName, id string
	}{
		{"google-gemini-veo.yingce-plugin", "gemini-veo"},
		{"novita-video.yingce-plugin", "novita-video"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			adapter := officialPackageAdapter(t, tc.packageName, tc.id)
			_, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
				Model: tc.id, Prompt: "keep the character", Images: []MediaReference{{URL: "https://cdn.example/character.png", Role: "reference_image"}},
			}})
			if err == nil || (!strings.Contains(err.Error(), "reference_to_video") && !strings.Contains(err.Error(), "角色参考图")) {
				t.Fatalf("BuildCreate() error = %v", err)
			}
		})
	}
}

func TestAsyncVideoResponseNormalizesStatusAndResult(t *testing.T) {
	adapter := officialPackageAdapter(t, "minimax-hailuo-video-v2.yingce-plugin", "minimax-video")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"task":{"id":"mm-1","status":"processing"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "mm-1" || created.Status != StatusProcessing {
		t.Fatalf("created = %#v", created)
	}
	polled, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: created.TaskID}, []byte(`{"task":{"id":"mm-1","status":"succeeded","content":{"url":"https://cdn.example/video.mp4"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if polled.Status != StatusSucceeded || polled.Result == nil || len(polled.Result.Videos) != 1 || polled.Result.Videos[0].URL != "https://cdn.example/video.mp4" {
		t.Fatalf("polled = %#v", polled)
	}
}

func TestDeclarativeManifestMapsFieldsAndResponses(t *testing.T) {
	manifest := []byte(`{
			"apiVersion":"yingce.plugin/v1",
			"id":"example-video","version":"1.0.0","name":"Example Video","author":"Example","documentation":"# Example Video",
		"contributes":{"providers":[{"id":"example-video","label":"Example Video","capabilities":["video"],"scopes":["admin.system-channel","user.custom-channel"],"create":{"method":"POST","path":"/v1/tasks","fields":{"model":"request.model","input.prompt":"request.prompt","input.seconds":"request.duration"}},"poll":{"method":"GET","path":"/v1/tasks/{{taskId}}"},"response":{"taskIdPaths":["id","data.id"],"statusPaths":["status","data.status"],"resultPaths":["result.video_url","data.result.video_url"],"resultKind":"video"}}]}
	}`)
	adapter, err := LoadManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "example", Prompt: "hello", Duration: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if create.Path != "/v1/tasks" || create.Method != http.MethodPost {
		t.Fatalf("create = %#v", create)
	}
	body, _ := json.Marshal(create.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	input := got["input"].(map[string]any)
	if input["prompt"] != "hello" || input["seconds"].(float64) != 5 {
		t.Fatalf("body = %#v", got)
	}
	poll, err := adapter.BuildPoll(context.Background(), PollContext{Model: "example", TaskID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if poll.Path != "/v1/tasks/task-1" {
		t.Fatalf("poll = %#v", poll)
	}
	result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-1"}, []byte(`{"data":{"id":"task-1","status":"succeeded","result":{"video_url":"https://cdn.example/clip.mp4"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSucceeded || result.Result == nil || len(result.Result.Videos) != 1 || result.Result.Videos[0].URL != "https://cdn.example/clip.mp4" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDeclarativeManifestSupportsMediaPathsTransformsAndErrors(t *testing.T) {
	manifest := []byte(`{
		"apiVersion":"yingce.plugin/v1",
		"id":"declarative-expression-test","version":"1.0.0","name":"Declarative Expression Test","author":"Test","documentation":"# Declarative Expression Test",
		"contributes":{"providers":[{"id":"declarative-expression-test","label":"Declarative Expression Test","capabilities":["video"],"scopes":["canvas"],"create":{"method":"POST","path":"/tasks","fields":{"resolution":"request.resolution|lower","ref_image_0":"request.images.0.url","ref_image_1":"request.images.1.url"}},"response":{"errorPaths":["code"],"messagePaths":["msg"]}}]}
	}`)
	adapter, err := LoadManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Resolution: "768P", Images: []MediaReference{{URL: "https://cdn.example/one.png"}}}})
	if err != nil {
		t.Fatal(err)
	}
	body := spec.Body.(map[string]any)
	if body["resolution"] != "768p" || body["ref_image_0"] != "https://cdn.example/one.png" {
		t.Fatalf("mapped body = %#v", body)
	}
	if _, exists := body["ref_image_1"]; exists {
		t.Fatalf("empty media path was not omitted: %#v", body)
	}
	failed, err := adapter.ParseCreate(context.Background(), []byte(`{"code":"RequestParameterIsWrong","msg":"invalid resolution"}`))
	if err != nil || failed.Status != StatusFailed || failed.Message != "invalid resolution" {
		t.Fatalf("failed response = %#v, err = %v", failed, err)
	}
}

func TestDeclarativeManifestKeepsLegacyVideoResolutionTransformCompatible(t *testing.T) {
	tests := map[string]string{
		"768":   "768p",
		"768P":  "768p",
		"768P横": "768p横",
	}
	for input, expected := range tests {
		if actual := applyManifestTransform(input, "video-resolution", GenerationRequest{}); actual != expected {
			t.Errorf("legacy video resolution transform %q = %#v, want %q", input, actual, expected)
		}
	}
}

func TestDeclarativeManifestRequiresDocumentation(t *testing.T) {
	manifest := Manifest{
		APIVersion: "v1",
		Metadata: Metadata{
			ID: "missing-docs", Version: "1.0.0", Name: "Missing Docs",
			Categories: []Capability{CapabilityVideo},
			Scopes:     []Surface{SurfaceAdminSystemChannel},
		},
		Create: ManifestOperation{Method: http.MethodPost, Path: "/v1/tasks"},
	}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("manifest without documentation was accepted")
	}
}

func TestImageResponseKeepsBase64AsDataURL(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-images.yingce-plugin", "openai-image")
	result, err := adapter.ParseCreate(context.Background(), []byte(`{"data":[{"b64_json":"aW1hZ2U="}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Result == nil || len(result.Result.Images) != 1 || result.Result.Images[0].URL != "" || result.Result.Images[0].DataURL != "data:image/png;base64,aW1hZ2U=" {
		t.Fatalf("images = %#v", result.Result)
	}
}

func TestOpenAIImagesEditUsesJSONImageReferences(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-images.yingce-plugin", "openai-image")
	if !adapter.Metadata().RequiresPublicMediaURLs {
		t.Fatal("OpenAI Images reference inputs must be hydrated as public URLs")
	}
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model:  "gpt-image-2.5",
		Prompt: "combine both references",
		Images: []MediaReference{
			{URL: "https://cdn.example/reference-1.png", Role: "edit_source", Order: 0},
			{URL: "https://cdn.example/reference-2.png", Role: "edit_source", Order: 1},
			{URL: "https://cdn.example/mask.png", Role: "mask", Order: 2},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != "/v1/images/edits" || spec.ContentType != "application/json" || len(spec.Files) != 0 {
		t.Fatalf("edit request = path:%q contentType:%q files:%#v", spec.Path, spec.ContentType, spec.Files)
	}
	body, ok := spec.Body.(map[string]any)
	if !ok {
		t.Fatalf("body = %#v", spec.Body)
	}
	images, ok := body["images"].([]any)
	if !ok || len(images) != 2 {
		t.Fatalf("images = %#v", body["images"])
	}
	for index, want := range []string{"https://cdn.example/reference-1.png", "https://cdn.example/reference-2.png"} {
		image, ok := images[index].(map[string]any)
		if !ok || image["image_url"] != want {
			t.Fatalf("images[%d] = %#v, want image_url %q", index, images[index], want)
		}
	}
	mask, ok := body["mask"].(map[string]any)
	if !ok || mask["image_url"] != "https://cdn.example/mask.png" {
		t.Fatalf("mask = %#v", body["mask"])
	}
	if _, exists := body["response_format"]; exists {
		t.Fatalf("response_format should not be sent by default: %#v", body["response_format"])
	}
}

func TestAsyncMediaPollKeepsResultKind(t *testing.T) {
	cases := []struct {
		packageName, id, payload, want string
	}{
		{"volcengine-jimeng-image.yingce-plugin", "volcengine-jimeng-image", `{"data":{"status":"completed","image_urls":["https://cdn.example/image.png"]}}`, "image"},
		{"async-audio.yingce-plugin", "async-audio", `{"id":"audio-1","status":"completed","audio_url":"https://cdn.example/audio.mp3"}`, "audio"},
		{"minimax-hailuo-video-v2.yingce-plugin", "minimax-video", `{"task":{"id":"video-1","status":"succeeded","content":{"url":"https://cdn.example/video.mp4"}}}`, "video"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			adapter := officialPackageAdapter(t, tc.packageName, tc.id)
			result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "fallback"}, []byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusSucceeded || result.Result == nil {
				t.Fatalf("result = %#v", result)
			}
			switch tc.want {
			case "image":
				if len(result.Result.Images) != 1 || len(result.Result.Videos) != 0 || len(result.Result.Audios) != 0 {
					t.Fatalf("media = %#v", result.Result)
				}
			case "audio":
				if len(result.Result.Audios) != 1 || len(result.Result.Images) != 0 || len(result.Result.Videos) != 0 {
					t.Fatalf("media = %#v", result.Result)
				}
			case "video":
				if len(result.Result.Videos) != 1 || len(result.Result.Images) != 0 || len(result.Result.Audios) != 0 {
					t.Fatalf("media = %#v", result.Result)
				}
			}
		})
	}
}

func TestGeminiVeoParsesLongRunningOperation(t *testing.T) {
	adapter := officialPackageAdapter(t, "google-gemini-veo.yingce-plugin", "gemini-veo")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"name":"operations/veo-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "operations/veo-1" || (created.Status != StatusPending && created.Status != StatusProcessing) {
		t.Fatalf("created = %#v", created)
	}

	result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: created.TaskID}, []byte(`{
		"name":"operations/veo-1",
		"done":true,
		"response":{"generatedVideos":[{"url":"https://cdn.example/veo.mp4"}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSucceeded || result.Result == nil || len(result.Result.Videos) != 1 || result.Result.Videos[0].URL != "https://cdn.example/veo.mp4" {
		t.Fatalf("result = %#v", result)
	}
}

func TestAsyncMediaPollRejectsUnsupportedCapability(t *testing.T) {
	if _, err := parseAsyncMediaPoll(PollContext{TaskID: "task-1"}, map[string]any{"status": "completed"}, CapabilityText); err == nil {
		t.Fatal("text capability was accepted by async media parser")
	}
}
