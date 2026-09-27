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

type providerFailureKind string

const (
	providerFailureHTTP     providerFailureKind = "http"
	providerFailureBusiness providerFailureKind = "business"
)

// interpretUpstream is the only host entry for upstream response errors
// (new-api RelayErrorHandler + ResetStatusCode in one call):
//  1. GeneralError field extract → providerFailure
//  2. declarative channel config via protocol.Interpret
//
// Non-2xx always failed. Plugin may mark ChannelPending; attachUpstreamTaskID
// turns that into Pending once a Provider Task ID is known.
// 2xx fails only on a business-error envelope (Kind=business, StatusCode kept).
// Adapter must already be bound on the outbound context (withProviderChannel);
// this function never looks up channels itself.
func interpretUpstream(adapter protocol.Adapter, statusCode int, body []byte) (failed bool, failure providerFailure) {
	failure = providerFailure{
		Kind:       providerFailureHTTP,
		StatusCode: statusCode,
		Body:       string(body),
	}
	fillGeneralErrorFields(&failure)

	if statusCode < 200 || statusCode >= 300 {
		if channel, ok := protocol.Interpret(adapter, statusCode, body); ok {
			applyChannelResponse(channel, &failure)
		}
		return true, failure
	}

	if channel, ok := protocol.Interpret(adapter, statusCode, body); ok {
		if !channel.Failed {
			return false, failure
		}
		if channel.Code != "" {
			failure.Code = channel.Code
		}
		if channel.Message != "" {
			failure.Message = channel.Message
		}
		failure.Kind = providerFailureBusiness
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
		failure.Kind = providerFailureBusiness
		return true, failure
	}
	return false, failure
}

// attachUpstreamTaskID enables Pending continuation without re-interpreting the body.
func attachUpstreamTaskID(err error, taskID string) error {
	var failure providerFailure
	if !errors.As(err, &failure) {
		return err
	}
	taskID = strings.TrimSpace(taskID)
	if !failure.ChannelPending || taskID == "" {
		return failure
	}
	failure.Pending = true
	failure.TaskID = taskID
	return failure
}

func analyticsFromContext(ctx context.Context) providerAnalyticsContext {
	metadata, _ := ctx.Value(providerAnalyticsKey{}).(providerAnalyticsContext)
	return metadata
}

func applyChannelResponse(channel protocol.ResponseError, failure *providerFailure) {
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
		failure.ChannelPending = true
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
	normalized := strings.ToLower(value)
	return strings.Contains(normalized, contentModerationErrorCode) ||
		strings.Contains(normalized, "inputtextsensitivecontentdetected")
}
