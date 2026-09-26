package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

const contentModerationErrorCode = "sensitive_words_detected"

const contentModerationRetryMessage = "内容审核未通过，请修改提示词后重新生成；原任务不能直接重试"

// enrichProviderFailure fills Code/Message from a JSON error body when present.
func enrichProviderFailure(err *providerFailure) {
	if err == nil || strings.TrimSpace(err.Body) == "" {
		return
	}
	var payload map[string]any
	if json.Unmarshal([]byte(err.Body), &payload) != nil {
		return
	}
	code, message := providerFailureDetails(payload)
	if err.Code == "" {
		err.Code = code
	}
	if err.Message == "" {
		err.Message = message
	}
}

// 只提取供应商明确返回的错误码和短消息，避免把完整响应或用户输入复制到调用日志。
func providerFailureDetails(payload map[string]any) (string, string) {
	candidates := make([]map[string]any, 0, 3)
	for _, key := range []string{"error", "data"} {
		if nested, ok := payload[key].(map[string]any); ok {
			candidates = append(candidates, nested)
		}
	}
	candidates = append(candidates, payload)
	code := ""
	message := ""
	for _, candidate := range candidates {
		if code == "" {
			code = normalizedProviderErrorCode(candidate["code"])
		}
		if message == "" {
			message = strings.TrimSpace(stringField(candidate, "message"))
			if message == "" {
				message = strings.TrimSpace(stringField(candidate, "msg"))
			}
		}
	}
	return code, truncateRunes(message, 500)
}

// openAICompatibleBusinessFailure recognizes only the generic OpenAI-style
// error object / top-level code. Vendor-specific nesting belongs in plugin
// errorPaths via protocol.BusinessFailure.
func openAICompatibleBusinessFailure(payload map[string]any) (string, string, bool) {
	if errorValue, ok := payload["error"].(map[string]any); ok {
		code, message := providerFailureDetails(map[string]any{"error": errorValue})
		if code != "" || message != "" {
			return code, message, true
		}
	}
	if !providerBusinessCodeFailed(payload["code"]) {
		return "", "", false
	}
	code, message := providerFailureDetails(payload)
	return code, message, true
}

func providerBusinessCodeFailed(value any) bool {
	code := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	switch code {
	case "", "0", "success", "succeeded", "ok", "<nil>":
		return false
	default:
		return true
	}
}

func normalizedProviderErrorCode(value any) string {
	var code string
	switch current := value.(type) {
	case string:
		code = current
	case fmt.Stringer:
		code = current.String()
	case float64:
		if current != 0 {
			code = fmt.Sprintf("%g", current)
		}
	case int:
		if current != 0 {
			code = fmt.Sprintf("%d", current)
		}
	case int64:
		if current != 0 {
			code = fmt.Sprintf("%d", current)
		}
	}
	code = strings.TrimSpace(code)
	if code == "0" {
		return ""
	}
	return truncateRunes(code, 80)
}

func isContentModerationFailure(value string) bool {
	return strings.Contains(strings.ToLower(value), contentModerationErrorCode)
}

func providerFailureFromMessage(raw string) providerFailure {
	raw = strings.TrimSpace(raw)
	return providerFailure{
		Code:    "",
		Message: truncateRunes(raw, 500),
		Body:    raw,
	}
}
