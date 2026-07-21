package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

func TestRewriteUpstreamErrorMessageBlacklist(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = map[string]string{
		ErrorRewriteEnabledKey:          "true",
		ErrorRewriteFallbackMessageKey:  "fallback error",
		ErrorRewriteRefreshSecondsKey:   "60",
		ErrorRewriteRequestTimeoutMSKey: "1000",
		ErrorRewriteRulesJSONKey:        `[{"content_contains":"aws","message":"masked upstream error","error_type":"upstream_service_error","error_code":"masked_code","error_param":"masked_param","status_code":418}]`,
	}
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
	}()

	errorRewriteCache.Lock()
	errorRewriteCache.rules = ErrorRewriteRules{
		Rules: []ErrorRewriteRule{
			{ContentContains: "aws", Type: "blacklist"},
		},
	}
	errorRewriteCache.fetchedAt = time.Now()
	errorRewriteCache.Unlock()

	if got := RewriteUpstreamErrorMessage("upstream aws credential error"); got != "masked upstream error" {
		t.Fatalf("expected blacklist match to be rewritten, got %q", got)
	}

	if got := RewriteUpstreamErrorMessage("request id: aws-12345678901234567890123456789012"); got != "request id: aws-12345678901234567890123456789012" {
		t.Fatalf("expected request id match to be ignored, got %q", got)
	}

	err := types.NewOpenAIError(errors.New("upstream aws credential error"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	RewriteNewAPIError(err)
	if err.StatusCode != http.StatusTeapot {
		t.Fatalf("expected status code to be rewritten, got %d", err.StatusCode)
	}
	openAIError, ok := err.RelayError.(types.OpenAIError)
	if !ok {
		t.Fatalf("expected OpenAI relay error, got %T", err.RelayError)
	}
	if openAIError.Type != "upstream_service_error" {
		t.Fatalf("expected error type to be rewritten, got %q", openAIError.Type)
	}
	if openAIError.Code != "masked_code" {
		t.Fatalf("expected error code to be rewritten, got %v", openAIError.Code)
	}
	if openAIError.Param != "" {
		t.Fatalf("expected empty upstream param to remain empty, got %q", openAIError.Param)
	}
}

func TestRewriteUpstreamErrorMessageFallsBackWhenNoLocalReplacement(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = map[string]string{
		ErrorRewriteEnabledKey:         "true",
		ErrorRewriteFallbackMessageKey: "fallback monitoring error",
		ErrorRewriteRulesJSONKey:       `[]`,
	}
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
	}()

	errorRewriteCache.Lock()
	errorRewriteCache.rules = ErrorRewriteRules{
		Rules: []ErrorRewriteRule{{ContentContains: "aws", Type: "blacklist"}},
	}
	errorRewriteCache.fetchedAt = time.Now()
	errorRewriteCache.Unlock()

	if got := RewriteUpstreamErrorMessage("upstream aws credential error"); got != "fallback monitoring error" {
		t.Fatalf("expected fallback message, got %q", got)
	}

	err := types.NewOpenAIError(errors.New("upstream aws credential error"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	RewriteNewAPIError(err)
	if err.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected original status code to be kept, got %d", err.StatusCode)
	}
}

func TestRewriteNewAPIErrorFieldModes(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = map[string]string{
		ErrorRewriteEnabledKey:         "true",
		ErrorRewriteFallbackMessageKey: "fallback error",
		ErrorRewriteRulesJSONKey:       `[{"content_contains":"aws","message":"masked error","error_type":"upstream_service_error","error_type_mode":"replace","error_code_mode":"filter","error_param":"masked_param","error_param_mode":"replace"}]`,
	}
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
	}()

	errorRewriteCache.Lock()
	errorRewriteCache.rules = ErrorRewriteRules{
		Rules: []ErrorRewriteRule{{ContentContains: "aws", Type: "blacklist"}},
	}
	errorRewriteCache.fetchedAt = time.Now()
	errorRewriteCache.Unlock()

	openAIErr := types.WithOpenAIError(types.OpenAIError{
		Message: "aws upstream failed",
		Type:    "model_not_found",
		Code:    "model_not_found",
		Param:   "",
	}, http.StatusBadGateway)
	RewriteNewAPIError(openAIErr)
	openAIError, ok := openAIErr.RelayError.(types.OpenAIError)
	if !ok {
		t.Fatalf("expected OpenAI relay error, got %T", openAIErr.RelayError)
	}
	if openAIError.Type != "upstream_service_error" {
		t.Fatalf("expected error type to be rewritten, got %q", openAIError.Type)
	}
	if openAIError.Code != nil {
		t.Fatalf("expected error code to be filtered, got %v", openAIError.Code)
	}
	if openAIError.Param != "" {
		t.Fatalf("expected empty upstream param to remain empty, got %q", openAIError.Param)
	}
	if claudeConverted := openAIErr.ToClaudeError(); claudeConverted.Type != "upstream_service_error" {
		t.Fatalf("expected converted Claude error type to use rewritten type, got %q", claudeConverted.Type)
	}

	claudeErr := types.WithClaudeError(types.ClaudeError{
		Message: "aws upstream failed",
		Type:    "model_not_found",
	}, http.StatusBadGateway)
	RewriteNewAPIError(claudeErr)
	claudeError, ok := claudeErr.RelayError.(types.ClaudeError)
	if !ok {
		t.Fatalf("expected Claude relay error, got %T", claudeErr.RelayError)
	}
	if claudeError.Type != "upstream_service_error" {
		t.Fatalf("expected Claude error type to be rewritten, got %q", claudeError.Type)
	}
}

func TestRewriteNewAPIErrorDefaultsCodeAndParamToFilter(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = map[string]string{
		ErrorRewriteEnabledKey:   "true",
		ErrorRewriteRulesJSONKey: `[{"content_contains":"aws","message":"masked error","error_type":"upstream_service_error","error_type_mode":"replace"}]`,
	}
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
	}()

	errorRewriteCache.Lock()
	errorRewriteCache.rules = ErrorRewriteRules{
		Rules: []ErrorRewriteRule{{ContentContains: "aws", Type: "blacklist"}},
	}
	errorRewriteCache.fetchedAt = time.Now()
	errorRewriteCache.Unlock()

	err := types.WithOpenAIError(types.OpenAIError{
		Message: "aws upstream failed",
		Type:    "model_not_found",
		Code:    "model_not_found",
		Param:   "model",
	}, http.StatusBadGateway)
	RewriteNewAPIError(err)
	openAIError, ok := err.RelayError.(types.OpenAIError)
	if !ok {
		t.Fatalf("expected OpenAI relay error, got %T", err.RelayError)
	}
	if openAIError.Code != nil {
		t.Fatalf("expected error code to be filtered by default, got %v", openAIError.Code)
	}
	if openAIError.Param != "" {
		t.Fatalf("expected error param to be filtered by default, got %q", openAIError.Param)
	}
}

func TestRewriteUpstreamErrorMessageMonitorRuleFields(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = map[string]string{
		ErrorRewriteEnabledKey:         "true",
		ErrorRewriteFallbackMessageKey: "fallback monitoring error",
		ErrorRewriteRulesJSONKey:       `[{"content_contains":"aws\nquota","message":"masked quota error"}]`,
	}
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
	}()

	errorRewriteCache.Lock()
	errorRewriteCache.rules = ErrorRewriteRules{
		Rules: []ErrorRewriteRule{{
			Type:            "blacklist",
			StatusCode:      http.StatusBadGateway,
			ChannelID:       7,
			GroupScope:      "vip",
			ModelName:       "gpt",
			ContentContains: "aws\nquota",
		}},
	}
	errorRewriteCache.fetchedAt = time.Now()
	errorRewriteCache.Unlock()

	ctx := ErrorRewriteMatchContext{
		StatusCode: http.StatusBadGateway,
		ChannelID:  7,
		GroupScope: "vip",
		ModelName:  "gpt-5",
	}
	replacement, ok := rewriteUpstreamError("aws account quota exceeded, request_id=aws-abcdefabcdefabcdefabcdefabcdefabcdef", ctx)
	if !ok || replacement.Message != "masked quota error" {
		t.Fatalf("expected full monitor rule match, got ok=%v rewritten=%q", ok, replacement.Message)
	}

	ctx.ChannelID = 8
	replacement, ok = rewriteUpstreamError("aws account quota exceeded", ctx)
	if ok || replacement.Message != "" {
		t.Fatalf("expected channel mismatch to skip rewrite, got ok=%v rewritten=%q", ok, replacement.Message)
	}
}
