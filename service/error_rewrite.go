package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ErrorRewriteEnabledKey          = "ErrorRewriteEnabled"
	ErrorRewriteSourceKey           = "ErrorRewriteSource"
	ErrorRewriteRulesJSONKey        = "ErrorRewriteRulesJSON"
	ErrorRewriteMonitorRulesJSONKey = "ErrorRewriteMonitorRulesJSON"
	ErrorRewriteMonitorVersionKey   = "ErrorRewriteMonitorRulesVersion"
	ErrorRewriteMonitorLastSyncKey  = "ErrorRewriteMonitorLastSyncAt"
	ErrorRewriteMonitorLastPullKey  = "ErrorRewriteMonitorLastPullAt"
	ErrorRewriteSyncTokenKey        = "ErrorRewriteSyncToken"
	ErrorRewriteRulesURLKey         = "ErrorRewriteRulesURL"
	ErrorRewriteFallbackMessageKey  = "ErrorRewriteFallbackMessage"
	ErrorRewriteRefreshSecondsKey   = "ErrorRewriteRefreshSeconds"
	ErrorRewriteRequestTimeoutMSKey = "ErrorRewriteRequestTimeoutMS"
	ErrorRewriteSQLDriverKey        = "ErrorRewriteSQLDriver"
	ErrorRewriteSQLDSNKey           = "ErrorRewriteSQLDSN"
	ErrorRewriteSQLQueryKey         = "ErrorRewriteSQLQuery"
)

type ErrorRewriteRule struct {
	ID              uint   `json:"id,omitempty"`
	Keyword         string `json:"keyword,omitempty"`
	Type            string `json:"type,omitempty"`
	Message         string `json:"message,omitempty"`
	ErrorType       string `json:"error_type,omitempty"`
	ErrorTypeMode   string `json:"error_type_mode,omitempty"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorCodeMode   string `json:"error_code_mode,omitempty"`
	ErrorParam      string `json:"error_param,omitempty"`
	ErrorParamMode  string `json:"error_param_mode,omitempty"`
	StatusCode      int    `json:"status_code,omitempty"`
	ChannelID       int    `json:"channel_id,omitempty"`
	ChannelName     string `json:"channel_name,omitempty"`
	ChannelGroup    string `json:"channel_group,omitempty"`
	GroupScope      string `json:"group_scope,omitempty"`
	ModelName       string `json:"model_name,omitempty"`
	ContentContains string `json:"content_contains,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	CreatedBy       string `json:"created_by,omitempty"`
}

type ErrorRewriteRules struct {
	Blacklist []string           `json:"blacklist"`
	Whitelist []string           `json:"whitelist"`
	Message   string             `json:"message"`
	Rules     []ErrorRewriteRule `json:"rules"`
}

type ErrorRewriteMonitorStats struct {
	Total   int64 `json:"total"`
	Enabled int64 `json:"enabled"`
}

type ErrorRewriteMonitorSyncPayload struct {
	Source  string             `json:"source"`
	Full    bool               `json:"full"`
	SentAt  int64              `json:"sent_at"`
	Version int64              `json:"version"`
	Rules   []ErrorRewriteRule `json:"rules"`
}

type ErrorRewriteMatchContext struct {
	StatusCode   int
	ChannelID    int
	ChannelName  string
	ChannelGroup string
	GroupScope   string
	ModelName    string
	ChannelInfo  *model.ChannelInfo
}

type errorRewriteReplacement struct {
	Message    string
	StatusCode int
	ErrorType  string
	TypeMode   string
	ErrorCode  string
	CodeMode   string
	ErrorParam string
	ParamMode  string
}

var errorRewriteCache = struct {
	sync.RWMutex
	rules     ErrorRewriteRules
	fetchedAt time.Time
}{}

var errorRewritePullTaskStarted bool

var (
	errorRewriteRequestIDPattern    = regexp.MustCompile(`(?i)\s*\(?\s*(request[\s_-]*id|requestid|req[\s_-]*id|reqid|trace[\s_-]*id|traceid|upstream[\s_-]*request[\s_-]*id|x[\s_-]*request[\s_-]*id|ref)\s*[:=]\s*[^),;\s]+[),]?`)
	errorRewriteStatusCodePattern   = regexp.MustCompile(`(?i)^\s*status[\s_-]*code\s*=\s*\d+\s*,?\s*`)
	errorRewriteUUIDPattern         = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	errorRewriteLongHexPattern      = regexp.MustCompile(`(?i)\b[0-9a-f]{24,}\b`)
	errorRewriteLongAlphaNumPattern = regexp.MustCompile(`\b[A-Za-z0-9_-]{32,}\b`)
	errorRewritePunctuationSpacing  = regexp.MustCompile(`\s*([,:;()])\s*`)
	errorRewriteWhitespacePattern   = regexp.MustCompile(`\s+`)
	errorRewriteRepeatedComma       = regexp.MustCompile(`\s*,\s*,+`)
	errorRewriteTrailingJunk        = regexp.MustCompile(`[\s,;:()]+$`)
	errorRewritePunctuationReplacer = strings.NewReplacer("，", ",", "：", ":", "；", ";", "（", "(", "）", ")", "＄", "$")
)

func DefaultErrorRewriteOptions() map[string]string {
	return map[string]string{
		ErrorRewriteEnabledKey:          "false",
		ErrorRewriteSourceKey:           "local",
		ErrorRewriteRulesJSONKey:        "[]",
		ErrorRewriteMonitorRulesJSONKey: "[]",
		ErrorRewriteMonitorVersionKey:   "0",
		ErrorRewriteMonitorLastSyncKey:  "0",
		ErrorRewriteMonitorLastPullKey:  "0",
		ErrorRewriteSyncTokenKey:        "",
		ErrorRewriteRulesURLKey:         "",
		ErrorRewriteFallbackMessageKey:  "request blocked by monitoring system",
		ErrorRewriteRefreshSecondsKey:   "60",
		ErrorRewriteRequestTimeoutMSKey: "3000",
		ErrorRewriteSQLDriverKey:        "mysql",
		ErrorRewriteSQLDSNKey:           "",
		ErrorRewriteSQLQueryKey:         "SELECT keyword, rule_type FROM error_rewrite_rules WHERE enabled = 1",
	}
}

func RewriteUpstreamErrorMessage(message string) string {
	replacement, ok := rewriteUpstreamError(message, ErrorRewriteMatchContext{})
	if !ok {
		return message
	}
	return replacement.Message
}

func rewriteUpstreamError(message string, matchCtx ErrorRewriteMatchContext) (errorRewriteReplacement, bool) {
	if strings.TrimSpace(message) == "" || !optionBool(ErrorRewriteEnabledKey) {
		return errorRewriteReplacement{}, false
	}
	if matchCtx.ChannelInfo != nil && !matchCtx.ChannelInfo.IsErrorRewriteEnabled() {
		return errorRewriteReplacement{}, false
	}
	monitorRules := currentErrorRewriteRules()
	localRules, err := ErrorRewriteRulesFromOptions()
	if err != nil {
		common.SysError("failed to parse local error rewrite rules: " + err.Error())
	}
	for _, rule := range monitorRules.Rules {
		if !monitorBlacklistRuleMatches(rule, message, matchCtx) {
			continue
		}
		key := ruleReplacementKey(rule)
		if replacement := replacementForKeyword(localRules, key); replacement.Message != "" {
			return replacement, true
		}
		return errorRewriteReplacement{Message: optionString(ErrorRewriteFallbackMessageKey, "request blocked by monitoring system")}, true
	}
	for _, word := range monitorRules.Blacklist {
		if containsFold(message, word) {
			if replacement := replacementForKeyword(localRules, word); replacement.Message != "" {
				return replacement, true
			}
			return errorRewriteReplacement{Message: optionString(ErrorRewriteFallbackMessageKey, "request blocked by monitoring system")}, true
		}
	}
	return errorRewriteReplacement{}, false
}

func RewriteNewAPIError(err *types.NewAPIError) {
	RewriteNewAPIErrorWithMatchContext(err, ErrorRewriteMatchContext{})
}

func RewriteNewAPIErrorWithGinContext(err *types.NewAPIError, c *gin.Context) {
	RewriteNewAPIErrorWithMatchContext(err, errorRewriteMatchContextFromGin(err, c))
}

func RewriteNewAPIErrorWithMatchContext(err *types.NewAPIError, matchCtx ErrorRewriteMatchContext) {
	if err == nil {
		return
	}
	if matchCtx.StatusCode == 0 {
		matchCtx.StatusCode = err.StatusCode
	}
	replacement, ok := rewriteUpstreamError(err.Error(), matchCtx)
	if !ok || replacement.Message == "" || replacement.Message == err.Error() {
		return
	}
	rewritten := replacement.Message
	err.SetMessage(rewritten)
	if isValidRewriteStatusCode(replacement.StatusCode) {
		err.StatusCode = replacement.StatusCode
	}
	switch relayErr := err.RelayError.(type) {
	case types.OpenAIError:
		relayErr.Message = rewritten
		relayErr.Metadata = nil
		relayErr.Type = rewriteStringField(relayErr.Type, replacement.TypeMode, replacement.ErrorType)
		relayErr.Code = rewriteAnyField(relayErr.Code, replacement.CodeMode, replacement.ErrorCode)
		relayErr.Param = rewriteStringField(relayErr.Param, replacement.ParamMode, replacement.ErrorParam)
		err.Metadata = nil
		err.RelayError = relayErr
	case types.ClaudeError:
		relayErr.Message = rewritten
		relayErr.Type = rewriteStringField(relayErr.Type, replacement.TypeMode, replacement.ErrorType)
		err.RelayError = relayErr
	}
}

func errorRewriteMatchContextFromGin(err *types.NewAPIError, c *gin.Context) ErrorRewriteMatchContext {
	ctx := ErrorRewriteMatchContext{}
	if err != nil {
		ctx.StatusCode = err.StatusCode
	}
	if c == nil {
		return ctx
	}
	ctx.ChannelID = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	ctx.ChannelName = common.GetContextKeyString(c, constant.ContextKeyChannelName)
	if ctx.ChannelID > 0 {
		if channel, err := model.CacheGetChannel(ctx.ChannelID); err == nil && channel != nil {
			ctx.ChannelInfo = &channel.ChannelInfo
		}
	}
	ctx.ModelName = common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	ctx.ChannelGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if ctx.ChannelGroup == "" {
		ctx.ChannelGroup = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}
	ctx.GroupScope = logGroupScope(ctx.ChannelGroup)
	return ctx
}

func ErrorRewriteEnabled() bool {
	return optionBool(ErrorRewriteEnabledKey)
}

func ErrorRewriteFallbackMessage() string {
	return optionString(ErrorRewriteFallbackMessageKey, "request blocked by monitoring system")
}

func CurrentErrorRewriteRulesForStatus() (ErrorRewriteRules, error) {
	if model.DB != nil {
		return ErrorRewriteMonitorRulesFromDB()
	}
	return ErrorRewriteMonitorRulesFromOptions()
}

func ErrorRewriteMonitorVersion() int64 {
	value := optionString(ErrorRewriteMonitorVersionKey, "0")
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func ErrorRewriteMonitorLastSyncAt() int64 {
	value := optionString(ErrorRewriteMonitorLastSyncKey, "0")
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func ErrorRewriteMonitorLastPullAt() int64 {
	value := optionString(ErrorRewriteMonitorLastPullKey, "0")
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func ErrorRewriteSyncToken() string {
	if token := strings.TrimSpace(optionString(ErrorRewriteSyncTokenKey, "")); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv("APM_LOG_BLACKLIST_SYNC_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("ERROR_REWRITE_SYNC_TOKEN"))
}

func StartErrorRewriteMonitorPullTask() {
	if errorRewritePullTaskStarted {
		return
	}
	errorRewritePullTaskStarted = true
	go func() {
		for {
			interval := optionInt(ErrorRewriteRefreshSecondsKey, 60)
			if interval < 1 {
				interval = 60
			}
			if strings.EqualFold(strings.TrimSpace(optionString(ErrorRewriteSourceKey, "local")), "http") {
				if err := PullErrorRewriteMonitorRulesOnce(); err != nil {
					common.SysError("failed to pull monitor blacklist rules: " + err.Error())
				}
			}
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}()
}

func PullErrorRewriteMonitorRulesOnce() error {
	if !strings.EqualFold(strings.TrimSpace(optionString(ErrorRewriteSourceKey, "local")), "http") {
		return fmt.Errorf("error rewrite source is not http")
	}
	rulesURL := strings.TrimSpace(optionString(ErrorRewriteRulesURLKey, ""))
	if rulesURL == "" {
		return fmt.Errorf("monitor rules URL is empty")
	}
	timeoutMS := optionInt(ErrorRewriteRequestTimeoutMSKey, 3000)
	if timeoutMS < 100 {
		timeoutMS = 100
	}
	req, err := http.NewRequest(http.MethodGet, rulesURL, nil)
	if err != nil {
		return err
	}
	if token := ErrorRewriteSyncToken(); token != "" {
		req.Header.Set("X-Blacklist-Sync-Token", token)
		req.Header.Set("X-Error-Rewrite-Sync-Token", token)
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("monitor rules endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	rules, version, err := parsePulledMonitorRules(body)
	if err != nil {
		return err
	}
	rulesJSON, err := NormalizeErrorRewriteMonitorRules(rules)
	if err != nil {
		return err
	}
	if err := SyncErrorRewriteMonitorRulesToDB(rules); err != nil {
		return err
	}
	if version <= 0 {
		version = time.Now().Unix()
	}
	if err := model.UpdateOptionsBulk(map[string]string{
		ErrorRewriteMonitorRulesJSONKey: rulesJSON,
		ErrorRewriteMonitorVersionKey:   strconv.FormatInt(version, 10),
		ErrorRewriteMonitorLastSyncKey:  strconv.FormatInt(time.Now().Unix(), 10),
		ErrorRewriteMonitorLastPullKey:  strconv.FormatInt(time.Now().Unix(), 10),
	}); err != nil {
		return err
	}
	ResetErrorRewriteMonitorCache()
	common.SysLog(fmt.Sprintf("monitor blacklist rules pulled: rules=%d", len(filterBlacklistRules(rules))))
	return nil
}

func parsePulledMonitorRules(body []byte) ([]ErrorRewriteRule, int64, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, 0, fmt.Errorf("monitor rules response is empty")
	}
	if body[0] == '[' {
		var rules []ErrorRewriteRule
		if err := json.Unmarshal(body, &rules); err != nil {
			return nil, 0, err
		}
		return rules, 0, nil
	}
	var payload struct {
		Success *bool              `json:"success"`
		Message string             `json:"message"`
		Source  string             `json:"source"`
		Full    bool               `json:"full"`
		SentAt  int64              `json:"sent_at"`
		Version int64              `json:"version"`
		Rules   []ErrorRewriteRule `json:"rules"`
		Items   []ErrorRewriteRule `json:"items"`
		Data    json.RawMessage    `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, 0, err
	}
	if payload.Success != nil && !*payload.Success {
		if strings.TrimSpace(payload.Message) != "" {
			return nil, 0, fmt.Errorf("%s", payload.Message)
		}
		return nil, 0, fmt.Errorf("monitor rules endpoint returned success=false")
	}
	version := payload.Version
	if version <= 0 {
		version = payload.SentAt
	}
	if len(payload.Rules) > 0 {
		return payload.Rules, version, nil
	}
	if len(payload.Items) > 0 {
		return payload.Items, version, nil
	}
	if len(bytes.TrimSpace(payload.Data)) > 0 {
		dataRules, dataVersion, err := parsePulledMonitorRules(payload.Data)
		if err != nil {
			return nil, 0, err
		}
		if dataVersion > 0 {
			version = dataVersion
		}
		return dataRules, version, nil
	}
	return []ErrorRewriteRule{}, version, nil
}

func currentErrorRewriteRules() ErrorRewriteRules {
	refreshSeconds := optionInt(ErrorRewriteRefreshSecondsKey, 60)
	if refreshSeconds < 1 {
		refreshSeconds = 60
	}
	errorRewriteCache.RLock()
	if time.Since(errorRewriteCache.fetchedAt) < time.Duration(refreshSeconds)*time.Second {
		rules := errorRewriteCache.rules
		errorRewriteCache.RUnlock()
		return rules
	}
	errorRewriteCache.RUnlock()

	errorRewriteCache.Lock()
	defer errorRewriteCache.Unlock()
	if time.Since(errorRewriteCache.fetchedAt) < time.Duration(refreshSeconds)*time.Second {
		return errorRewriteCache.rules
	}
	errorRewriteCache.fetchedAt = time.Now()
	rules, err := fetchErrorRewriteRules()
	if err != nil {
		common.SysError("failed to fetch error rewrite rules: " + err.Error())
		return errorRewriteCache.rules
	}
	errorRewriteCache.rules = rules
	return rules
}

func fetchErrorRewriteRules() (ErrorRewriteRules, error) {
	if model.DB != nil {
		return ErrorRewriteMonitorRulesFromDB()
	}
	return ErrorRewriteMonitorRulesFromOptions()
}

func ErrorRewriteRulesFromOptions() (ErrorRewriteRules, error) {
	rules, err := ParseErrorRewriteRulesJSON(optionString(ErrorRewriteRulesJSONKey, "[]"))
	if err != nil {
		return ErrorRewriteRules{}, err
	}
	return ErrorRewriteRules{Rules: rules}, nil
}

func ErrorRewriteMonitorRulesFromOptions() (ErrorRewriteRules, error) {
	rules, err := ParseErrorRewriteMonitorRulesJSON(optionString(ErrorRewriteMonitorRulesJSONKey, "[]"))
	if err != nil {
		return ErrorRewriteRules{}, err
	}
	return ErrorRewriteRules{Rules: rules}, nil
}

func ErrorRewriteMonitorRulesFromDB() (ErrorRewriteRules, error) {
	if model.DB == nil {
		return ErrorRewriteRules{}, nil
	}
	var rows []model.LogBlacklistRule
	if err := model.DB.Where("enabled = ?", true).Order("id ASC").Find(&rows).Error; err != nil {
		return ErrorRewriteRules{}, err
	}
	rules := make([]ErrorRewriteRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, errorRewriteRuleFromDB(row))
	}
	return ErrorRewriteRules{Rules: rules}, nil
}

func ErrorRewriteAllMonitorRulesFromDB() (ErrorRewriteRules, error) {
	if model.DB == nil {
		return ErrorRewriteMonitorRulesFromOptions()
	}
	var rows []model.LogBlacklistRule
	if err := model.DB.Order("id ASC").Find(&rows).Error; err != nil {
		return ErrorRewriteRules{}, err
	}
	rules := make([]ErrorRewriteRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, errorRewriteRuleFromDB(row))
	}
	return ErrorRewriteRules{Rules: rules}, nil
}

func ErrorRewriteMonitorStatsFromDB() (ErrorRewriteMonitorStats, error) {
	if model.DB == nil {
		rules, err := ErrorRewriteMonitorRulesFromOptions()
		if err != nil {
			return ErrorRewriteMonitorStats{}, err
		}
		return ErrorRewriteMonitorStats{Total: int64(len(rules.Rules)), Enabled: int64(len(rules.Rules))}, nil
	}
	var total int64
	if err := model.DB.Model(&model.LogBlacklistRule{}).Count(&total).Error; err != nil {
		return ErrorRewriteMonitorStats{}, err
	}
	var enabled int64
	if err := model.DB.Model(&model.LogBlacklistRule{}).Where("enabled = ?", true).Count(&enabled).Error; err != nil {
		return ErrorRewriteMonitorStats{}, err
	}
	return ErrorRewriteMonitorStats{Total: total, Enabled: enabled}, nil
}

func SyncErrorRewriteMonitorRulesToDB(rules []ErrorRewriteRule) error {
	if model.DB == nil {
		return fmt.Errorf("database is not initialized")
	}
	incomingIDs := make(map[uint]bool, len(rules))
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var existing []model.LogBlacklistRule
		if err := tx.Find(&existing).Error; err != nil {
			return err
		}
		for _, rule := range filterBlacklistRules(rules) {
			row := dbLogBlacklistRuleFromErrorRewriteRule(rule)
			if row.ID == 0 {
				continue
			}
			incomingIDs[row.ID] = true
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"status_code",
					"channel_id",
					"channel_name",
					"channel_group",
					"group_scope",
					"model_name",
					"content_contains",
					"enabled",
					"created_by",
					"created_at",
					"updated_at",
				}),
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		for _, rule := range existing {
			if incomingIDs[rule.ID] {
				continue
			}
			if err := tx.Delete(&model.LogBlacklistRule{}, rule.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func ParseErrorRewriteMonitorRulesJSON(raw string) ([]ErrorRewriteRule, error) {
	rules, err := parseErrorRewriteRulesJSON(raw, false)
	if err != nil {
		return nil, err
	}
	return filterBlacklistRules(rules), nil
}

func NormalizeErrorRewriteMonitorRules(rules []ErrorRewriteRule) (string, error) {
	for _, rule := range rules {
		if strings.TrimSpace(ruleContentContains(rule)) == "" {
			return "", fmt.Errorf("content_contains cannot be empty")
		}
		for _, segment := range blacklistContentSegments(ruleContentContains(rule)) {
			if len([]rune(strings.TrimSpace(segment))) < 2 {
				return "", fmt.Errorf("content_contains segment must be at least 2 characters")
			}
		}
	}
	normalized := filterBlacklistRules(rules)
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func ParseErrorRewriteRulesJSON(raw string) ([]ErrorRewriteRule, error) {
	return parseErrorRewriteRulesJSON(raw, true)
}

func parseErrorRewriteRulesJSON(raw string, allowLocalFields bool) ([]ErrorRewriteRule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "[]"
	}
	var rules []ErrorRewriteRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if strings.TrimSpace(ruleContentContains(rule)) == "" {
			return nil, fmt.Errorf("content_contains cannot be empty")
		}
		if allowLocalFields && rule.StatusCode != 0 && !isValidRewriteStatusCode(rule.StatusCode) {
			return nil, fmt.Errorf("status_code must be between 100 and 599")
		}
	}
	return rules, nil
}

func filterBlacklistRules(rules []ErrorRewriteRule) []ErrorRewriteRule {
	result := make([]ErrorRewriteRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(rule.Type), "blacklist") || strings.TrimSpace(rule.ContentContains) != "" {
			rule.Type = "blacklist"
			rule.Message = ""
			if rule.ContentContains == "" {
				rule.ContentContains = strings.TrimSpace(rule.Keyword)
			}
			result = append(result, rule)
		}
	}
	return result
}

func ResetErrorRewriteMonitorCache() {
	errorRewriteCache.Lock()
	errorRewriteCache.rules = ErrorRewriteRules{}
	errorRewriteCache.fetchedAt = time.Time{}
	errorRewriteCache.Unlock()
}

func replacementForKeyword(rules ErrorRewriteRules, keyword string) errorRewriteReplacement {
	for _, rule := range rules.Rules {
		if strings.EqualFold(strings.TrimSpace(ruleReplacementKey(rule)), strings.TrimSpace(keyword)) {
			return errorRewriteReplacement{
				Message:    strings.TrimSpace(rule.Message),
				StatusCode: rule.StatusCode,
				ErrorType:  strings.TrimSpace(rule.ErrorType),
				TypeMode:   normalizeRewriteFieldMode(rule.ErrorTypeMode, rule.ErrorType),
				ErrorCode:  strings.TrimSpace(rule.ErrorCode),
				CodeMode:   normalizeRewriteFieldModeWithDefault(rule.ErrorCodeMode, rule.ErrorCode, "filter"),
				ErrorParam: strings.TrimSpace(rule.ErrorParam),
				ParamMode:  normalizeRewriteFieldModeWithDefault(rule.ErrorParamMode, rule.ErrorParam, "filter"),
			}
		}
	}
	return errorRewriteReplacement{}
}

func normalizeRewriteFieldModeWithDefault(mode string, value string, defaultMode string) string {
	if strings.TrimSpace(mode) == "" && strings.TrimSpace(value) == "" {
		return normalizeRewriteFieldMode(defaultMode, "")
	}
	return normalizeRewriteFieldMode(mode, value)
}

func normalizeRewriteFieldMode(mode string, value string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "filter", "remove", "delete", "过滤":
		return "filter"
	case "keep", "preserve", "保留":
		return "keep"
	case "replace", "替换":
		return "replace"
	default:
		if strings.TrimSpace(value) != "" {
			return "replace"
		}
		return "keep"
	}
}

func rewriteStringField(current string, mode string, replacement string) string {
	switch normalizeRewriteFieldMode(mode, replacement) {
	case "filter":
		return ""
	case "replace":
		if strings.TrimSpace(current) != "" && strings.TrimSpace(replacement) != "" {
			return strings.TrimSpace(replacement)
		}
	}
	return current
}

func rewriteAnyField(current any, mode string, replacement string) any {
	switch normalizeRewriteFieldMode(mode, replacement) {
	case "filter":
		return nil
	case "replace":
		if current != nil && strings.TrimSpace(fmt.Sprintf("%v", current)) != "" && strings.TrimSpace(replacement) != "" {
			return strings.TrimSpace(replacement)
		}
	}
	return current
}

func ruleContentContains(rule ErrorRewriteRule) string {
	if strings.TrimSpace(rule.ContentContains) != "" {
		return strings.TrimSpace(rule.ContentContains)
	}
	return strings.TrimSpace(rule.Keyword)
}

func ruleReplacementKey(rule ErrorRewriteRule) string {
	return ruleContentContains(rule)
}

func errorRewriteRuleFromDB(row model.LogBlacklistRule) ErrorRewriteRule {
	enabled := row.Enabled
	return ErrorRewriteRule{
		ID:              row.ID,
		Type:            "blacklist",
		StatusCode:      row.StatusCode,
		ChannelID:       row.ChannelID,
		ChannelName:     row.ChannelName,
		ChannelGroup:    row.ChannelGroup,
		GroupScope:      row.GroupScope,
		ModelName:       row.ModelName,
		ContentContains: row.ContentContains,
		Enabled:         &enabled,
	}
}

func dbLogBlacklistRuleFromErrorRewriteRule(rule ErrorRewriteRule) model.LogBlacklistRule {
	enabled := true
	if rule.Enabled != nil {
		enabled = *rule.Enabled
	}
	return model.LogBlacklistRule{
		ID:              rule.ID,
		StatusCode:      rule.StatusCode,
		ChannelID:       rule.ChannelID,
		ChannelName:     strings.TrimSpace(rule.ChannelName),
		ChannelGroup:    strings.TrimSpace(rule.ChannelGroup),
		GroupScope:      strings.TrimSpace(rule.GroupScope),
		ModelName:       strings.TrimSpace(rule.ModelName),
		ContentContains: ruleContentContains(rule),
		Enabled:         enabled,
		CreatedBy:       strings.TrimSpace(rule.CreatedBy),
	}
}

func monitorBlacklistRuleMatches(rule ErrorRewriteRule, message string, matchCtx ErrorRewriteMatchContext) bool {
	if rule.Enabled != nil && !*rule.Enabled {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(rule.Type), "blacklist") && strings.TrimSpace(rule.ContentContains) == "" {
		return false
	}
	if rule.StatusCode > 0 && matchCtx.StatusCode > 0 && rule.StatusCode != matchCtx.StatusCode {
		return false
	}
	ruleScope := strings.TrimSpace(rule.GroupScope)
	if ruleScope == "" {
		ruleScope = logGroupScope(rule.ChannelGroup)
	}
	ctxScope := strings.TrimSpace(matchCtx.GroupScope)
	if ctxScope == "" {
		ctxScope = logGroupScope(matchCtx.ChannelGroup)
	}
	if ruleScope != "" && ctxScope != "" && ruleScope != ctxScope {
		return false
	}
	if rule.ChannelID > 0 && matchCtx.ChannelID > 0 && rule.ChannelID != matchCtx.ChannelID {
		return false
	}
	if strings.TrimSpace(rule.ModelName) != "" && strings.TrimSpace(matchCtx.ModelName) != "" &&
		!strings.Contains(strings.ToLower(matchCtx.ModelName), strings.ToLower(strings.TrimSpace(rule.ModelName))) {
		return false
	}
	segments := validBlacklistContentSegments(ruleContentContains(rule))
	if len(segments) == 0 {
		return false
	}
	displayContent := blacklistMatchContent(message)
	for _, segment := range segments {
		needle := strings.ToLower(strings.TrimSpace(segment))
		if needle == "" {
			continue
		}
		if !strings.Contains(displayContent, needle) {
			return false
		}
	}
	return true
}

func blacklistContentSegments(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var jsonParts []string
	if strings.HasPrefix(raw, "[") && json.Unmarshal([]byte(raw), &jsonParts) == nil {
		parts := make([]string, 0, len(jsonParts))
		for _, part := range jsonParts {
			if value := strings.TrimSpace(part); value != "" {
				parts = append(parts, value)
			}
		}
		return parts
	}
	lines := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if value := strings.TrimSpace(line); value != "" {
			parts = append(parts, value)
		}
	}
	return parts
}

func validBlacklistContentSegments(raw string) []string {
	segments := blacklistContentSegments(raw)
	valid := make([]string, 0, len(segments))
	for _, segment := range segments {
		if len([]rune(strings.TrimSpace(segment))) >= 2 {
			valid = append(valid, segment)
		}
	}
	return valid
}

func blacklistMatchContent(content string) string {
	return strings.ToLower(cleanLogContentForDisplay(content))
}

func cleanLogContentForDisplay(content string) string {
	cleaned := normalizeLogContentBase(content)
	cleaned = errorRewriteRepeatedComma.ReplaceAllString(cleaned, ",")
	cleaned = errorRewriteTrailingJunk.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}

func normalizeLogContentBase(content string) string {
	s := strings.TrimSpace(content)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = errorRewritePunctuationReplacer.Replace(s)
	s = errorRewriteStatusCodePattern.ReplaceAllString(s, "")
	s = errorRewriteRequestIDPattern.ReplaceAllString(s, " ")
	s = errorRewriteUUIDPattern.ReplaceAllString(s, " ")
	s = errorRewriteLongHexPattern.ReplaceAllString(s, " ")
	s = errorRewriteLongAlphaNumPattern.ReplaceAllString(s, " ")
	s = errorRewritePunctuationSpacing.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "\n", " ")
	s = errorRewriteWhitespacePattern.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func logGroupScope(channelGroup string) string {
	channelGroup = strings.TrimSpace(channelGroup)
	if channelGroup == "" {
		return ""
	}
	return strings.ToLower(channelGroup)
}

func isValidRewriteStatusCode(statusCode int) bool {
	return statusCode >= 100 && statusCode <= 599
}

func containsFold(haystack string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func optionBool(key string) bool {
	return optionString(key, "false") == "true"
}

func optionInt(key string, fallback int) int {
	value := optionString(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func optionString(key string, fallback string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	if value, ok := common.OptionMap[key]; ok {
		return value
	}
	return fallback
}
