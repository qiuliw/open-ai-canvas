package app

import (
	"encoding/json"
	"regexp"
	"strings"
)

var providerErrorURL = regexp.MustCompile(`(?i)https?://[^\s<>"'，。；）]+`)
var providerErrorSensitive = regexp.MustCompile(`(?i)余额|额度|配额|欠费|账单|充值|计费|密钥|密码|令牌|balance|quota|billing|credit|payment|funds|wallet|api[ _-]?key|authorization|bearer|cookie|(?:access|refresh|auth|session)[ _-]?token|token\s*[=:]|secret|password|credential|sk-[a-z0-9]|tenant|trace[ _-]?id|request[ _-]?id|internal stack|stack\s*trace|dial tcp|no such host|prompt\s*[=:]`)

// providerSafeUpstreamMessage 对齐 NewAPI RelayErrorHandler：从正文抠 message 并做敏感清洗。
// 宿主不做供应商错误码归类；可读文案由渠道插件 response.message 映射，否则透传上游 message。
func providerSafeUpstreamMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	message := generalUpstreamErrorMessage(raw)
	if message == "" {
		if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
			var payload any
			if json.Unmarshal([]byte(raw), &payload) != nil {
				return ""
			}
			message = providerErrorMessageField(payload)
		} else {
			message = raw
		}
	}
	return providerSanitizeErrorMessage(message)
}

// generalUpstreamErrorMessage 对齐 NewAPI GeneralErrorResponse.ToMessage 字段嗅探。
func generalUpstreamErrorMessage(raw string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return ""
	}
	if errValue, ok := payload["error"]; ok {
		switch typed := errValue.(type) {
		case string:
			if msg := strings.TrimSpace(typed); msg != "" {
				return msg
			}
		case map[string]any:
			if msg, ok := typed["message"].(string); ok {
				if msg = strings.TrimSpace(msg); msg != "" {
					return msg
				}
			}
		}
	}
	for _, key := range []string{"message", "msg", "err", "error_msg", "detail"} {
		if msg, ok := payload[key].(string); ok {
			if msg = strings.TrimSpace(msg); msg != "" {
				return msg
			}
		}
	}
	if header, ok := payload["header"].(map[string]any); ok {
		if msg, ok := header["message"].(string); ok {
			if msg = strings.TrimSpace(msg); msg != "" {
				return msg
			}
		}
	}
	if response, ok := payload["response"].(map[string]any); ok {
		if errObj, ok := response["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok {
				if msg = strings.TrimSpace(msg); msg != "" {
					return msg
				}
			}
		}
	}
	return ""
}

func providerSanitizeErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" || strings.HasPrefix(message, "{") || strings.HasPrefix(message, "[") || strings.ContainsAny(message, "<>") || providerErrorSensitive.MatchString(message) {
		return ""
	}
	message = providerErrorURL.ReplaceAllString(message, "[链接已隐藏]")
	return truncateRunes(strings.Join(strings.Fields(message), " "), 1_500)
}

// 只提取错误消息字段，不序列化整个响应，避免回传 headers、请求正文和诊断信息。
func providerErrorDetail(raw string) string {
	return providerSafeUpstreamMessage(raw)
}

func providerErrorMessageField(value any) string {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		for _, key := range []string{"error", "message", "msg", "detail"} {
			if message := providerErrorMessageField(value[key]); message != "" {
				return message
			}
		}
	}
	return ""
}

func providerErrorWithDetail(fallback, raw string) string {
	if message := providerSafeUpstreamMessage(raw); message != "" {
		return message
	}
	return fallback
}

func appendProviderErrorDetail(fallback, raw string) string {
	if detail := providerErrorDetail(raw); detail != "" && detail != fallback {
		return fallback + "；上游：" + detail
	}
	return fallback
}

func providerPayloadErrorMessage(raw string) string {
	return providerErrorWithDetail("模型服务返回失败，请检查请求内容或渠道配置", raw)
}
