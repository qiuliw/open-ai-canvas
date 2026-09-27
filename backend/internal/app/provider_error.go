package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/protocol"
)

const contentModerationErrorCode = "sensitive_words_detected"

const contentModerationRetryMessage = "内容审核未通过，请修改提示词后重新生成；原任务不能直接重试"

// interpretUpstream is the only host entry for upstream response errors
// (new-api RelayErrorHandler + ResetStatusCode in one call):
//  1. GeneralError field extract → providerFailure
//  2. declarative channel config (errorPaths / httpErrors / statusCodeMapping)
//
// Non-2xx always failed (Pending only when plugin says so and taskID is set).
// 2xx fails only on a business-error envelope.
func interpretUpstream(adapter protocol.Adapter, taskID string, statusCode int, body []byte) (failed bool, failure providerFailure) {
	failure = providerFailure{StatusCode: statusCode, Body: string(body)}
	fillGeneralErrorFields(&failure)

	if statusCode < 200 || statusCode >= 300 {
		if channel, ok := protocol.InterpretResponse(adapter, statusCode, body); ok {
			applyChannelResponse(taskID, channel, &failure)
		}
		return true, failure
	}

	if channel, ok := protocol.InterpretResponse(adapter, statusCode, body); ok {
		if !channel.Failed {
			return false, failure
		}
		if channel.Code != "" {
			failure.Code = channel.Code
		}
		if channel.Message != "" {
			failure.Message = channel.Message
		}
		// 2xx business envelope is not an HTTP-class failure (avoids "HTTP 200" / upstream_200).
		failure.StatusCode = 0
		return true, failure
	}

	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return false, failure
	}
	if code, message, ok := businessEnvelope(payload); ok {
		if code != "" {
			failure.Code = code
		}
		if message != "" {
			failure.Message = message
		}
		failure.StatusCode = 0
		return true, failure
	}
	return false, failure
}

// interpretUpstreamErr re-interprets an HTTP-layer providerFailure with channel
// config (taskID enables Pending → continue original poll).
func interpretUpstreamErr(adapter protocol.Adapter, taskID string, err error) error {
	var failure providerFailure
	if !errors.As(err, &failure) {
		return err
	}
	retryAfter := failure.RetryAfter
	status := failure.Status
	_, out := interpretUpstream(adapter, taskID, failure.StatusCode, []byte(failure.Body))
	out.RetryAfter = retryAfter
	out.Status = status
	return out
}

func analyticsFromContext(ctx context.Context) providerAnalyticsContext {
	metadata, _ := ctx.Value(providerAnalyticsKey{}).(providerAnalyticsContext)
	return metadata
}

func applyChannelResponse(taskID string, channel protocol.ResponseError, failure *providerFailure) {
	if failure == nil {
		return
	}
	if strings.TrimSpace(channel.Code) != "" {
		failure.Code = strings.TrimSpace(channel.Code)
	}
	if strings.TrimSpace(channel.Message) != "" {
		failure.Message = strings.TrimSpace(channel.Message)
	}
	if channel.Pending {
		if strings.TrimSpace(taskID) == "" {
			return
		}
		failure.Pending = true
		failure.TaskID = taskID
		return
	}
	if channel.MappedStatusCode > 0 {
		failure.StatusCode = channel.MappedStatusCode
	}
}

func fillGeneralErrorFields(err *providerFailure) {
	if err == nil || strings.TrimSpace(err.Body) == "" {
		return
	}
	var payload map[string]any
	if json.Unmarshal([]byte(err.Body), &payload) != nil {
		return
	}
	code, message := generalErrorFields(payload)
	if err.Code == "" {
		err.Code = code
	}
	if err.Message == "" {
		err.Message = message
	}
}

// generalErrorFields mirrors new-api GeneralErrorResponse.ToMessage / TryToOpenAIError.
func generalErrorFields(payload map[string]any) (string, string) {
	code := ""
	message := ""

	if errorRaw, ok := payload["error"]; ok {
		switch typed := errorRaw.(type) {
		case map[string]any:
			code = normalizedProviderErrorCode(typed["code"])
			message = firstNonEmptyTrim(
				stringField(typed, "message"),
				stringField(typed, "msg"),
			)
		case string:
			message = strings.TrimSpace(typed)
		}
	}

	if code == "" {
		code = normalizedProviderErrorCode(payload["code"])
	}
	if message == "" {
		message = firstNonEmptyTrim(
			stringField(payload, "message"),
			stringField(payload, "msg"),
			stringField(payload, "err"),
			stringField(payload, "error_msg"),
			stringField(payload, "detail"),
		)
	}
	if message == "" {
		if header, ok := payload["header"].(map[string]any); ok {
			message = strings.TrimSpace(stringField(header, "message"))
		}
	}
	if message == "" {
		if response, ok := payload["response"].(map[string]any); ok {
			if errObj, ok := response["error"].(map[string]any); ok {
				message = strings.TrimSpace(stringField(errObj, "message"))
				if code == "" {
					code = normalizedProviderErrorCode(errObj["code"])
				}
			}
		}
	}

	return code, truncateRunes(message, 500)
}

func firstNonEmptyTrim(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

// businessEnvelope detects a business-error JSON envelope when no declarative
// plugin is bound — same shapes as new-api GeneralErrorResponse.
func businessEnvelope(payload map[string]any) (string, string, bool) {
	if errorValue, ok := payload["error"].(map[string]any); ok {
		code, message := generalErrorFields(map[string]any{"error": errorValue})
		if code != "" || message != "" {
			return code, message, true
		}
	}
	if !businessCodeFailed(payload["code"]) {
		return "", "", false
	}
	code, message := generalErrorFields(payload)
	return code, message, true
}

func businessCodeFailed(value any) bool {
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
