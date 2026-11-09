package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/protocol"
)

type ModelCallMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Explicit fields keep credentials, transport options and task metadata outside
// the external API's authority. The existing admission resolves execution config.
type ModelCallParameters struct {
	MaxOutputTokens int    `json:"maxOutputTokens,omitempty"`
	Thinking        bool   `json:"thinking,omitempty"`
	AspectRatio     string `json:"aspectRatio,omitempty"`
	Resolution      string `json:"resolution,omitempty"`
	Quality         string `json:"quality,omitempty"`
	Count           int    `json:"count,omitempty"`
	Duration        int    `json:"duration,omitempty"`
	GenerateAudio   *bool  `json:"generateAudio,omitempty"`
	Watermark       *bool  `json:"watermark,omitempty"`
	Voice           string `json:"voice,omitempty"`
	AudioFormat     string `json:"audioFormat,omitempty"`
	AudioSpeed      string `json:"audioSpeed,omitempty"`
}

type CreateModelCallRequest struct {
	Model      string              `json:"model"`
	Capability string              `json:"capability,omitempty"`
	Prompt     string              `json:"prompt,omitempty"`
	Messages   []ModelCallMessage  `json:"messages,omitempty"`
	Parameters ModelCallParameters `json:"parameters,omitempty"`
	TraceID    string              `json:"-"`
	RequestID  string              `json:"-"`
}

type ModelCall struct {
	ID         string           `json:"id"`
	Model      string           `json:"model"`
	Capability string           `json:"capability"`
	Status     protocol.Status  `json:"status"`
	Result     *protocol.Result `json:"result,omitempty"`
	Reason     string           `json:"reason,omitempty"`
}

type ExternalModel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Capability string `json:"capability"`
}

func (s *Service) ExternalModels(userID, keyID string) ([]ExternalModel, error) {
	if err := s.requireModelCallKey(userID, keyID); err != nil {
		return nil, err
	}
	user, err := s.repo.User(userID)
	if err != nil {
		return nil, err
	}
	catalog, err := s.ModelCatalogForUser(user, nil)
	if err != nil {
		return nil, err
	}
	result := make([]ExternalModel, 0)
	for _, channel := range catalog.Channels {
		for _, m := range channel.Models {
			if m.Available {
				result = append(result, ExternalModel{ID: channel.ID + "::" + m.ModelKey, Name: m.DisplayName, Capability: m.Capability})
			}
		}
	}
	return result, nil
}

func modelCallTaskRequest(req CreateModelCallRequest) (CreateTaskRequest, error) {
	capability := strings.TrimSpace(req.Capability)
	if capability == "" {
		capability = "text"
	}
	switch capability {
	case "text", "image", "video", "audio":
	default:
		return CreateTaskRequest{}, BadAuthRequest("capability 必须为 text、image、video 或 audio")
	}
	channel, modelKey, ok := strings.Cut(strings.TrimSpace(req.Model), "::")
	if !ok || strings.TrimSpace(channel) == "" || strings.TrimSpace(modelKey) == "" {
		return CreateTaskRequest{}, BadAuthRequest("model 必须是 /api/model-calls/models 返回的 channelId::modelKey")
	}
	p := req.Parameters
	if p.MaxOutputTokens < 0 || p.MaxOutputTokens > 1_000_000 || p.Count < 0 || p.Count > 100 || p.Duration < 0 || p.Duration > 3600 {
		return CreateTaskRequest{}, BadAuthRequest("生成参数超出允许范围")
	}
	if capability != "text" && (len(req.Messages) > 0 || p.MaxOutputTokens != 0 || p.Thinking) {
		return CreateTaskRequest{}, BadAuthRequest("messages 和文本参数仅适用于 text")
	}
	history := make([]providerTextMessage, 0, len(req.Messages))
	var instructions []string
	for _, msg := range req.Messages {
		if (msg.Role != "system" && msg.Role != "user" && msg.Role != "assistant") || strings.TrimSpace(msg.Content) == "" {
			return CreateTaskRequest{}, BadAuthRequest("消息须包含有效 role 与非空文本 content")
		}
		history = append(history, providerTextMessage{Role: msg.Role, Content: msg.Content})
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" && len(history) > 0 {
		last := history[len(history)-1]
		if last.Role != "user" {
			return CreateTaskRequest{}, BadAuthRequest("未指定 prompt 时最后一条消息须为 user")
		}
		prompt = last.Content
		history = history[:len(history)-1]
	}
	if prompt == "" {
		return CreateTaskRequest{}, BadAuthRequest("必须提供 prompt 或用户消息")
	}
	config := map[string]any{"channelId": channel, "model": modelKey}
	filtered := history[:0]
	for _, msg := range history {
		if msg.Role == "system" {
			instructions = append(instructions, msg.Content)
		} else {
			filtered = append(filtered, msg)
		}
	}
	history = filtered
	if len(instructions) > 0 {
		config["systemPrompt"] = strings.Join(instructions, "\n\n")
	}
	if p.AspectRatio != "" {
		config["size"] = p.AspectRatio
	}
	if p.Resolution != "" {
		if capability == "image" {
			config["imageResolution"] = p.Resolution
		} else {
			config["vquality"] = p.Resolution
		}
	}
	if p.Quality != "" {
		config["quality"] = p.Quality
	}
	if p.Count > 0 {
		config["count"] = strconv.Itoa(p.Count)
	}
	if p.Duration > 0 {
		config["videoSeconds"] = strconv.Itoa(p.Duration)
	}
	if capability == "video" {
		if p.GenerateAudio != nil {
			config["videoGenerateAudio"] = strconv.FormatBool(*p.GenerateAudio)
		}
		if p.Watermark != nil {
			config["videoWatermark"] = strconv.FormatBool(*p.Watermark)
		}
	}
	if capability == "audio" {
		config["audioVoice"] = p.Voice
		config["audioFormat"] = p.AudioFormat
		config["audioSpeed"] = p.AudioSpeed
	}
	thinkingLevel := ThinkingLevelOff
	if p.Thinking {
		thinkingLevel = ThinkingLevelMedium
	}
	input := map[string]any{"mode": capability, "prompt": prompt, "config": config, "textHistory": history, "textOptions": canvasTextOptions{MaxOutputTokens: p.MaxOutputTokens, ThinkingLevel: thinkingLevel}, "metadata": map[string]any{"source": "external-api"}}
	return CreateTaskRequest{Type: "canvas_" + capability, Prompt: prompt, Model: modelKey, Input: input, TraceID: req.TraceID, RequestID: req.RequestID}, nil
}

// Admission and execution must share the same effective limit. The finance
// estimator reads config.maxOutputTokens; the worker reads textOptions.
func (s *Service) normalizeModelCallTextLimit(input map[string]any) error {
	if input["mode"] != "text" {
		return nil
	}
	config, _ := input["config"].(map[string]any)
	cm, err := s.repo.ChannelModelByKey(stringValue(config["channelId"]), stringValue(config["model"]))
	if err != nil {
		return err
	}
	profile, err := normalizedChannelModelCapability(cm, s.protocolRegistry())
	if err != nil {
		return err
	}
	limit := 4096
	if profile != nil && profile.Text != nil && profile.Text.MaxOutputTokens > 0 {
		limit = profile.Text.MaxOutputTokens
	}
	options, _ := input["textOptions"].(map[string]any)
	if options == nil {
		options = make(map[string]any)
		input["textOptions"] = options
	}
	requested, _ := strconv.Atoi(fmt.Sprint(options["maxOutputTokens"]))
	if requested > 0 && requested < limit {
		limit = requested
	}
	limit = min(limit, 131072)
	if wire := thinkingWireForProtocol(string(cm.Protocol), s.protocolRegistry()); wire == protocol.TextWireClaude {
		plan, err := resolveTextThinkingPlan(canvasGenerationInput{
			Config:      providerConfig{CapabilityConfig: profile},
			TextOptions: canvasTextOptions{ThinkingLevel: stringValue(options["thinkingLevel"])},
		}, wire)
		if err != nil {
			return err
		}
		if plan.enabled() && plan.Config.Intensity.Kind == ThinkingIntensityBudget && limit <= thinkingBudgetFor(plan.Config, plan.Level, limit) {
			return BadAuthRequest("Claude 思考预算必须小于有效 maxOutputTokens")
		}
	}
	config["maxOutputTokens"] = limit
	options["maxOutputTokens"] = limit
	return nil
}

func (s *Service) CreateModelCall(userID, keyID string, req CreateModelCallRequest) (*ModelCall, error) {
	if err := s.requireModelCallKey(userID, keyID); err != nil {
		return nil, err
	}
	taskReq, err := modelCallTaskRequest(req)
	if err != nil {
		return nil, err
	}
	taskReq.apiKeyID = keyID
	task, err := s.CreateTask(userID, taskReq)
	if err != nil {
		return nil, err
	}
	return externalModelCall(*task), nil
}

func (s *Service) modelCallTask(userID, keyID, id string) (*model.Task, error) {
	if err := s.requireModelCallKey(userID, keyID); err != nil {
		return nil, err
	}
	task, err := s.repo.TaskForUser(userID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("模型调用不存在")
	}
	if err != nil {
		return nil, err
	}
	if task.APIKeyID != keyID {
		return nil, NotFound("模型调用不存在")
	}
	return task, nil
}

func (s *Service) ModelCall(userID, keyID, id string) (*ModelCall, error) {
	task, err := s.modelCallTask(userID, keyID, id)
	if err != nil {
		return nil, err
	}
	return externalModelCall(*task), nil
}

func externalModelCall(task model.Task) *ModelCall {
	result := &ModelCall{ID: task.ID, Model: task.Model, Capability: strings.TrimPrefix(task.Type, "canvas_"), Status: protocol.Status(task.Status)}
	switch task.Status {
	case model.TaskStatusQueued:
		result.Status = protocol.StatusPending
	case model.TaskStatusRunning:
		result.Status = protocol.StatusProcessing
	case model.TaskStatusFailed, model.TaskStatusCancelled:
		result.Reason = taskForOutput(task).Error
	case model.TaskStatusSucceeded:
		var raw map[string]any
		_ = json.Unmarshal([]byte(task.ResultJSON), &raw)
		result.Result = &protocol.Result{Text: stringValue(raw["text"]), Reasoning: stringValue(raw["reasoning"])}
		result.Result.Images = externalCallMedia(task.ID, raw["images"])
		result.Result.Videos = externalCallMedia(task.ID, raw["video"])
		result.Result.Audios = externalCallMedia(task.ID, raw["audios"])
	}
	return result
}

func externalCallMedia(callID string, value any) []protocol.MediaReference {
	var values []any
	switch v := value.(type) {
	case []any:
		values = v
	case map[string]any:
		values = []any{v}
	}
	result := make([]protocol.MediaReference, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		id := stringValue(item["resourceId"])
		if id == "" {
			continue
		}
		result = append(result, protocol.MediaReference{ID: id, URL: "/api/model-calls/" + url.PathEscape(callID) + "/media/" + url.PathEscape(id), MIMEType: stringValue(item["mimeType"])})
	}
	return result
}

func (s *Service) PrepareModelCallMediaDelivery(userID, keyID, callID, resourceID string, options ResourceAccessOptions, rangeHeader string) (*ResourceDelivery, error) {
	call, err := s.ModelCall(userID, keyID, callID)
	if err != nil {
		return nil, err
	}
	if call.Result != nil {
		for _, items := range [][]protocol.MediaReference{call.Result.Images, call.Result.Videos, call.Result.Audios} {
			for _, item := range items {
				if item.ID == resourceID {
					return s.PrepareResourceDelivery(userID, resourceID, options, rangeHeader)
				}
			}
		}
	}
	return nil, NotFound("模型调用产物不存在")
}
