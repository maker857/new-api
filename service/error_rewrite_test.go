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
		ErrorRewriteRulesJSONKey:        `[{"content_contains":"aws","message":"masked upstream error","status_code":418}]`,
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
	rewritten, _, ok := rewriteUpstreamError("aws account quota exceeded, request_id=aws-abcdefabcdefabcdefabcdefabcdefabcdef", ctx)
	if !ok || rewritten != "masked quota error" {
		t.Fatalf("expected full monitor rule match, got ok=%v rewritten=%q", ok, rewritten)
	}

	ctx.ChannelID = 8
	rewritten, _, ok = rewriteUpstreamError("aws account quota exceeded", ctx)
	if ok || rewritten != "aws account quota exceeded" {
		t.Fatalf("expected channel mismatch to skip rewrite, got ok=%v rewritten=%q", ok, rewritten)
	}
}
