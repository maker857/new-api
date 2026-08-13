package volcengine

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	rootdto "github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type nativeTTSHTTPResponse struct {
	Code    int           `json:"code"`
	Message string        `json:"message,omitempty"`
	Usage   *v3UsageStats `json:"usage,omitempty"`
}

// HandleNativeTTSHTTP proxies the Volcengine native v3 NDJSON stream without
// translating its records into the OpenAI audio response shape.
func HandleNativeTTSHTTP(c *gin.Context, requestURL string, request *rootdto.VolcengineTTSNativeRequest, info *relaycommon.RelayInfo, cfg dto.VolcTTSConfig) (*dto.Usage, *types.NewAPIError) {
	if request == nil || len(request.RawBody) == 0 {
		return nil, types.NewErrorWithStatusCode(errors.New("volcengine native tts request body is empty"), types.ErrorCodeBadRequestBody, http.StatusBadRequest)
	}
	if info == nil || info.ChannelMeta == nil {
		return nil, types.NewErrorWithStatusCode(errors.New("volcengine native tts channel metadata is missing"), types.ErrorCodeInvalidApiType, http.StatusInternalServerError)
	}
	if err := cfg.Validate(); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeChannelInvalidKey, http.StatusUnauthorized)
	}

	connectID := uuid.NewString()
	if incomingConnectID := c.GetHeader("X-Api-Connect-Id"); isSafeV3RequestID(incomingConnectID) {
		connectID = strings.TrimSpace(incomingConnectID)
	}
	headers, err := buildV3AuthHeaders(info.ApiKey, cfg, connectID)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeChannelInvalidKey, http.StatusUnauthorized)
	}
	requestID := uuid.NewString()
	if incomingRequestID := c.GetHeader("X-Api-Request-Id"); isSafeV3RequestID(incomingRequestID) {
		requestID = strings.TrimSpace(incomingRequestID)
	}
	headers.Set("X-Api-Request-Id", requestID)
	headers.Set("Content-Type", "application/json")
	diagnosticBody, diagnosticFlow := service.PrepareDiagnosticOutboundRequest(c, info, http.MethodPost, requestURL, headers, bytes.NewReader(request.RawBody))
	upstreamRequest, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, requestURL, diagnosticBody)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("build volcengine native tts request: %w", err), types.ErrorCodeBadRequestBody, http.StatusInternalServerError)
	}
	upstreamRequest.Header = headers

	client, err := newV3StreamingHTTPClient(info.ChannelSetting.Proxy)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadRequestBody, http.StatusBadRequest)
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(upstreamRequest)
	if err != nil {
		service.RecordDiagnosticOutboundFailure(diagnosticFlow, err)
		if contextErr := c.Request.Context().Err(); contextErr != nil {
			return nil, newNativeTTSCancellationError(contextErr)
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("volcengine native tts request failed: %w", err), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	service.WrapDiagnosticOutboundNDJSONResponse(response, diagnosticFlow, v3HTTPMaxLineSize, nativeCredentialSecrets(info.ApiKey)...)
	defer response.Body.Close()
	if logID := response.Header.Get("X-Tt-Logid"); logID != "" {
		c.Header("X-Volc-Logid", logID)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		errorBody, _ := io.ReadAll(io.LimitReader(response.Body, v3MaxErrorBodySize))
		detail := redactV3CredentialText(string(errorBody), info.ApiKey)
		downstreamErrorBody := []byte(detail)
		contentType := response.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		c.Header("Content-Type", contentType)
		c.Status(response.StatusCode)
		if len(downstreamErrorBody) > 0 {
			_, _ = c.Writer.Write(downstreamErrorBody)
		}
		c.Set("native_response_committed", true)
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("volcengine native tts http status %d: %s", response.StatusCode, detail), types.ErrorCodeBadResponseStatusCode, response.StatusCode, types.ErrOptionWithSkipRetry())
	}

	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Header("Content-Type", contentType)
	c.Header("Transfer-Encoding", "chunked")
	var usageStats *v3UsageStats
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, v3HTTPInitialLineBuffer), v3HTTPMaxLineSize)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		message := nativeTTSHTTPResponse{}
		if err = common.Unmarshal(trimmed, &message); err != nil {
			if c.GetBool("native_response_committed") {
				return nil, types.NewErrorWithStatusCode(fmt.Errorf("parse volcengine native tts response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
			}
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("parse volcengine native tts response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		if message.Usage != nil {
			usageStats = message.Usage
		}
		outLine := line
		if message.Code != 0 && message.Code != v3HTTPCompleteCode {
			outLine = []byte(redactV3CredentialText(string(line), info.ApiKey))
		}
		written, writeErr := c.Writer.Write(append(outLine, '\n'))
		if written > 0 {
			c.Set("native_response_committed", true)
		}
		if writeErr != nil {
			if contextErr := c.Request.Context().Err(); contextErr != nil {
				return nil, newNativeTTSCancellationError(contextErr)
			}
			if c.GetBool("native_response_committed") {
				return nil, types.NewErrorWithStatusCode(fmt.Errorf("write volcengine native tts response: %w", writeErr), types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
			}
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("write volcengine native tts response: %w", writeErr), types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		c.Writer.Flush()
		if message.Code != 0 && message.Code != v3HTTPCompleteCode {
			detail := message.Message
			if len(detail) > v3MaxErrorBodySize {
				detail = detail[:v3MaxErrorBodySize]
			}
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("volcengine native tts provider error: code=%d message=%s", message.Code, detail), types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
		}
	}
	if err = scanner.Err(); err != nil {
		if contextErr := c.Request.Context().Err(); contextErr != nil {
			return nil, newNativeTTSCancellationError(contextErr)
		}
		if c.GetBool("native_response_committed") {
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("read volcengine native tts response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("read volcengine native tts response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	usage, clamp, err := parseV3UsageStatsChecked(usageStats, info.GetEstimatePromptTokens())
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	info.QuotaClamp = clamp
	return usage, nil
}

func isSafeV3RequestID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func newNativeTTSCancellationError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		fmt.Errorf("volcengine native tts request canceled: %w", err),
		types.ErrorCodeBadResponse,
		499,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func nativeCredentialSecrets(apiKey string) []string {
	secrets := []string{apiKey}
	for _, part := range strings.Split(apiKey, "|") {
		if strings.TrimSpace(part) != "" {
			secrets = append(secrets, strings.TrimSpace(part))
		}
	}
	return secrets
}
