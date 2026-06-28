package model

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateRequestFileLogUsesCLIProxyAPIStyleSections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLogDir := *common.LogDir
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	t.Cleanup(func() {
		*common.LogDir = originalLogDir
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?debug=1", strings.NewReader(`{"model":"gpt-test","messages":[]}`))
	ctx.Request.Header.Set("Authorization", "Bearer secret")
	ctx.Request.Header.Set("Content-Type", "application/json")

	log := &Log{
		UserId:            7,
		CreatedAt:         1700000000,
		Type:              LogTypeError,
		Content:           "upstream failed",
		ModelName:         "gpt-test",
		PromptTokens:      11,
		CompletionTokens:  3,
		UseTime:           2,
		IsStream:          true,
		ChannelId:         9,
		Group:             "default",
		RequestId:         "req:bad/id",
		UpstreamRequestId: "upstream-1",
		Other:             `{"request_path":"/v1/chat/completions"}`,
	}
	common.SetContextKey(ctx, constant.ContextKeyChannelName, "OpenAI/Primary")

	path, err := createRequestFileLog(ctx, log)
	require.NoError(t, err)
	createdAt := time.Unix(log.CreatedAt, 0)
	assert.Equal(t, filepath.Join(tempDir, "OpenAI-Primary", createdAt.Format("2006-01-02"), createdAt.Format("150405.000000000")+"-req-bad-id.log"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)

	assert.Contains(t, text, "=== REQUEST INFO ===")
	assert.Contains(t, text, "URL: /v1/chat/completions?debug=1")
	assert.Contains(t, text, "Authorization: ***masked***")
	assert.Contains(t, text, "=== REQUEST BODY ===")
	assert.Contains(t, text, `{"model":"gpt-test","messages":[]}`)
	assert.Contains(t, text, "=== API REQUEST ===")
	assert.Contains(t, text, "Model: gpt-test")
	assert.Contains(t, text, "=== API RESPONSE ===")
	assert.Contains(t, text, "Status: error")
	assert.Contains(t, text, "=== RESPONSE ===")
	assert.Contains(t, text, "upstream failed")
	assert.NotContains(t, text, "Bearer secret")
}

func TestCompactRequestLogBodyCompactsJSONOnly(t *testing.T) {
	assert.Equal(t, []byte(`{"model":"gpt-test","messages":[]}`), compactRequestLogBody([]byte("{\n  \"model\": \"gpt-test\",\n  \"messages\": []\n}")))
	assert.Equal(t, []byte("plain text"), compactRequestLogBody([]byte("  plain text\n")))
}

func TestWriteRequestFileLogEnsuresRequestIdBeforeSaving(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLogDir := *common.LogDir
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	t.Cleanup(func() {
		*common.LogDir = originalLogDir
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-test"}`))

	log := &Log{
		CreatedAt: 1700000000,
		Type:      LogTypeConsume,
		Content:   "ok",
		ModelName: "gpt-test",
		Other:     `{"request_path":"/v1/chat/completions"}`,
	}

	writeRequestFileLog(ctx, log)

	require.NotEmpty(t, log.RequestId)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	path, ok := other[requestLogPathOtherKey].(string)
	require.True(t, ok)
	require.FileExists(t, path)
	assert.Contains(t, path, filepath.Join(tempDir, "unknown-channel", "2023-11-15"))
}

func TestWriteRequestFileLogRespectsChannelSaveRequestLogSetting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLogDir := *common.LogDir
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	t.Cleanup(func() {
		*common.LogDir = originalLogDir
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test"}`))
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})

	log := &Log{
		CreatedAt: 1700000000,
		Type:      LogTypeConsume,
		Content:   "ok",
		ModelName: "gpt-test",
		Other:     `{"request_path":"/v1/responses"}`,
	}

	writeRequestFileLog(ctx, log)

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	assert.NotContains(t, other, requestLogPathOtherKey)
	_, err = os.Stat(filepath.Join(tempDir, "channel-0"))
	assert.True(t, os.IsNotExist(err))

	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{SaveRequestLog: true})
	writeRequestFileLog(ctx, log)

	other, err = common.StrToMap(log.Other)
	require.NoError(t, err)
	path, ok := other[requestLogPathOtherKey].(string)
	require.True(t, ok)
	require.FileExists(t, path)
}

func TestRequestFileLogChannelDirUsesOtherChannelName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLogDir := *common.LogDir
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	t.Cleanup(func() {
		*common.LogDir = originalLogDir
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test"}`))

	log := &Log{
		CreatedAt: 1700000000,
		Type:      LogTypeError,
		Content:   "failed",
		ModelName: "gpt-test",
		ChannelId: 12,
		RequestId: "req-other-channel",
		Other:     `{"channel_name":"Claude/API"}`,
	}

	path, err := createRequestFileLog(ctx, log)
	require.NoError(t, err)
	createdAt := time.Unix(log.CreatedAt, 0)
	assert.Equal(t, filepath.Join(tempDir, "Claude-API", createdAt.Format("2006-01-02"), createdAt.Format("150405.000000000")+"-req-other-channel.log"), path)
}

func TestRequestFileLogChannelDirKeepsChineseChannelName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLogDir := *common.LogDir
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	t.Cleanup(func() {
		*common.LogDir = originalLogDir
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test"}`))
	common.SetContextKey(ctx, constant.ContextKeyChannelName, "测试")

	log := &Log{
		CreatedAt: 1700000000,
		Type:      LogTypeConsume,
		Content:   "ok",
		ModelName: "gpt-test",
		ChannelId: 1,
		RequestId: "req-chinese-channel",
	}

	path, err := createRequestFileLog(ctx, log)
	require.NoError(t, err)
	createdAt := time.Unix(log.CreatedAt, 0)
	assert.Equal(t, filepath.Join(tempDir, "测试", createdAt.Format("2006-01-02"), createdAt.Format("150405.000000000")+"-req-chinese-channel.log"), path)
}

func TestCreateRequestFileLogPrunesOldRequestLogsByMaxSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLogDir := *common.LogDir
	originalMaxSize := common.RequestLogMaxSizeMB
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	common.RequestLogMaxSizeMB = 1
	t.Cleanup(func() {
		*common.LogDir = originalLogDir
		common.RequestLogMaxSizeMB = originalMaxSize
	})

	oldPath := filepath.Join(tempDir, "旧渠道", "2023-11-14", "old.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(oldPath), 0755))
	require.NoError(t, os.WriteFile(oldPath, make([]byte, 1024*1024+1), 0644))
	require.NoError(t, os.Chtimes(oldPath, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test"}`))
	common.SetContextKey(ctx, constant.ContextKeyChannelName, "测试")

	log := &Log{
		CreatedAt: 1700000000,
		Type:      LogTypeConsume,
		Content:   "ok",
		ModelName: "gpt-test",
		ChannelId: 1,
		RequestId: "req-prune",
	}

	path, err := createRequestFileLog(ctx, log)
	require.NoError(t, err)
	assert.NoFileExists(t, oldPath)
	assert.FileExists(t, path)
}

func TestCreateRequestFileLogUsesCapturedHTTPResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLogDir := *common.LogDir
	tempDir := t.TempDir()
	*common.LogDir = tempDir
	t.Cleanup(func() {
		*common.LogDir = originalLogDir
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test"}`))
	ctx.Writer.Header().Set("Content-Type", "application/json")
	ctx.Writer.Header().Set("Set-Cookie", "session=secret")
	ctx.Writer.WriteHeader(http.StatusCreated)
	capture := common.NewRequestLogResponseCapture(ctx.Writer.Header(), 1024)
	capture.AppendBody([]byte("{\n  \"id\": \"resp_1\",\n  \"status\": \"completed\"\n}"))
	ctx.Set(common.KeyRequestLogResponseCapture, capture)

	log := &Log{
		CreatedAt:        1700000000,
		Type:             LogTypeConsume,
		Content:          "completed",
		ModelName:        "gpt-test",
		PromptTokens:     11,
		CompletionTokens: 3,
		ChannelId:        1,
		Group:            "default",
		RequestId:        "req-http",
	}

	path, err := createRequestFileLog(ctx, log)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)

	assert.Contains(t, text, "=== RESPONSE ===")
	assert.Contains(t, text, "Status: 201 Created")
	assert.Contains(t, text, "Headers:")
	assert.Contains(t, text, "Content-Type: application/json")
	assert.Contains(t, text, "Set-Cookie: ***masked***")
	assert.Contains(t, text, `{"id":"resp_1","status":"completed"}`)
	assert.NotContains(t, text, "=== RESPONSE ===\nStatus: completed")
	assert.NotContains(t, text, "session=secret")
}
