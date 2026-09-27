package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

func normalizeStatus(raw string) Status {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "queued", "pending", "created", "submitted", "in_queue", "task_status_queued":
		return StatusPending
	case "running", "processing", "in_progress", "executing", "task_status_running":
		return StatusProcessing
	case "succeeded", "success", "completed", "complete", "done", "task_status_succeed":
		return StatusSucceeded
	case "cancelled", "canceled", "aborted":
		return StatusCancelled
	case "failed", "failure", "error", "expired":
		return StatusFailed
	default:
		return ""
	}
}

func decodeObject(body []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("protocol response is not JSON: %w", err)
	}
	return payload, nil
}

func object(value any) map[string]any { result, _ := value.(map[string]any); return result }

func mergeMaps(primary, fallback map[string]any) map[string]any {
	result := make(map[string]any, len(fallback)+len(primary))
	for k, v := range fallback {
		result[k] = v
	}
	for k, v := range primary {
		result[k] = v
	}
	return result
}

func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parseAsyncMediaPoll(c PollContext, payload map[string]any, capability Capability) (PollResult, error) {
	if capability != CapabilityImage && capability != CapabilityVideo && capability != CapabilityAudio {
		return PollResult{}, fmt.Errorf("unsupported async media capability %q", capability)
	}
	status := normalizeStatus(firstString(payload, "status", "state", "task_status"))
	if task := object(payload["task"]); task != nil {
		status = normalizeStatus(defaultValue(firstString(task, "status", "state"), string(status)))
		payload = mergeMaps(task, payload)
	}
	if data := object(payload["data"]); data != nil {
		payload = mergeMaps(data, payload)
		status = normalizeStatus(defaultValue(firstString(data, "status", "state"), string(status)))
	}
	if status == "" {
		status = StatusProcessing
	}
	result := &Result{}
	resultKind := string(capability)
	references := mediaFromArray(payload[resultKind+"s"], capability)
	references = append(references, mediaFromArray(payload["data"], capability)...)
	if content := object(payload["content"]); content != nil {
		references = append(references, mediaFromArray([]any{content}, capability)...)
	}
	keys := []string{resultKind + "_url", resultKind + "Url", "result_url", "url"}
	if resultKind == "video" {
		keys = append(keys, "object")
	}
	if value := firstString(payload, keys...); value != "" {
		references = append(references, MediaReference{URL: value, Kind: resultKind})
	}
	switch capability {
	case CapabilityImage:
		result.Images = references
	case CapabilityAudio:
		result.Audios = references
	case CapabilityVideo:
		result.Videos = references
	}
	if status == StatusSucceeded && len(references) == 0 {
		result = nil
	}
	return PollResult{TaskID: defaultValue(firstString(payload, "id", "task_id", "taskId"), c.TaskID), Status: status, Result: result, Message: firstString(payload, "message", "error", "fail_reason")}, nil
}

func dataMIME(value string) string {
	if index := strings.Index(value, ";"); strings.HasPrefix(value, "data:") && index > 5 {
		return strings.TrimPrefix(value[5:index], "")
	}
	return "application/octet-stream"
}

func dataPayload(value string) string {
	if index := strings.Index(value, ","); strings.HasPrefix(value, "data:") && index >= 0 {
		return value[index+1:]
	}
	return value
}

func mediaFromArray(value any, capability Capability) []MediaReference {
	values, _ := value.([]any)
	result := make([]MediaReference, 0, len(values))
	kind := string(capability)
	for _, item := range values {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			result = append(result, MediaReference{URL: strings.TrimSpace(value), Kind: kind})
			continue
		}
		if m, ok := item.(map[string]any); ok {
			if capability == CapabilityImage {
				if value := firstString(m, "b64_json", "data"); value != "" {
					if !strings.HasPrefix(value, "data:") {
						value = "data:image/png;base64," + value
					}
					result = append(result, MediaReference{DataURL: value, Kind: kind})
					continue
				}
			}
			if value := firstString(m, "url", "image_url", "video_url", "audio_url"); value != "" {
				result = append(result, MediaReference{URL: value, Kind: kind})
			}
		}
	}
	return result
}
