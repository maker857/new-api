package volcengine

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	rootdto "github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const volcASRV3BaseEndpoint = "https://openspeech.bytedance.com/api/v3/auc/bigmodel"

func getV3ASREndpoint(path string) string {
	if strings.HasSuffix(strings.TrimSpace(path), "/query") {
		return volcASRV3BaseEndpoint + "/query"
	}
	return volcASRV3BaseEndpoint + "/submit"
}

func buildV3ASRAuthHeaders(apiKey string, cfg dto.VolcASRConfig, requestID string) (http.Header, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("volcengine asr api key is required")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, errors.New("volcengine asr request id is required")
	}

	headers := make(http.Header)
	switch cfg.EffectiveAuthMode() {
	case dto.VolcASRAuthModeNewConsole:
		if strings.Contains(apiKey, "|") {
			return nil, errors.New("invalid volcengine asr api key format for new_console auth")
		}
		headers.Set("X-Api-Key", apiKey)
	case dto.VolcASRAuthModeLegacy:
		parts := strings.Split(apiKey, "|")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, errors.New("invalid volcengine asr api key format for legacy auth")
		}
		headers.Set("X-Api-App-Key", strings.TrimSpace(parts[0]))
		headers.Set("X-Api-Access-Key", strings.TrimSpace(parts[1]))
	default:
		return nil, fmt.Errorf("unsupported volcengine asr auth mode: %s", cfg.AuthMode)
	}

	headers.Set("X-Api-Resource-Id", cfg.ResourceID)
	headers.Set("X-Api-Request-Id", requestID)
	headers.Set("X-Api-Sequence", "-1")
	headers.Set("Content-Type", "application/json")
	return headers, nil
}

func HandleNativeASRHTTP(c *gin.Context, request *rootdto.VolcengineASRNativeRequest, info *relaycommon.RelayInfo, cfg dto.VolcASRConfig) *types.NewAPIError {
	return handleNativeASRHTTP(c, getV3ASREndpoint(c.Request.URL.Path), request, info, cfg)
}

func handleNativeASRHTTP(c *gin.Context, requestURL string, request *rootdto.VolcengineASRNativeRequest, info *relaycommon.RelayInfo, cfg dto.VolcASRConfig) *types.NewAPIError {
	if request == nil || len(request.RawBody) == 0 {
		return types.NewErrorWithStatusCode(errors.New("volcengine native asr request body is empty"), types.ErrorCodeBadRequestBody, http.StatusBadRequest)
	}
	if info == nil || info.ChannelMeta == nil {
		return types.NewErrorWithStatusCode(errors.New("volcengine native asr channel metadata is missing"), types.ErrorCodeInvalidApiType, http.StatusInternalServerError)
	}

	requestID := uuid.NewString()
	if incomingRequestID := c.GetHeader("X-Api-Request-Id"); isSafeV3RequestID(incomingRequestID) {
		requestID = strings.TrimSpace(incomingRequestID)
	}
	headers, err := buildV3ASRAuthHeaders(info.ApiKey, cfg, requestID)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeChannelInvalidKey, http.StatusUnauthorized)
	}

	diagnosticBody, diagnosticFlow := service.PrepareDiagnosticOutboundRequest(c, info, http.MethodPost, requestURL, headers, bytes.NewReader(request.RawBody))
	upstreamRequest, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, requestURL, diagnosticBody)
	if err != nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("build volcengine native asr request: %w", err), types.ErrorCodeBadRequestBody, http.StatusInternalServerError)
	}
	upstreamRequest.Header = headers

	client, err := newV3StreamingHTTPClient(info.ChannelSetting.Proxy)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeBadRequestBody, http.StatusBadRequest)
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(upstreamRequest)
	if err != nil {
		service.RecordDiagnosticOutboundFailure(diagnosticFlow, err)
		if contextErr := c.Request.Context().Err(); contextErr != nil {
			return newNativeASRCancellationError(contextErr)
		}
		return types.NewErrorWithStatusCode(fmt.Errorf("volcengine native asr request failed: %w", err), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	service.WrapDiagnosticOutboundResponse(c, response, diagnosticFlow)
	defer response.Body.Close()

	for key, values := range response.Header {
		canonicalKey := http.CanonicalHeaderKey(key)
		if canonicalKey != "Content-Type" && !strings.HasPrefix(canonicalKey, "X-Api-") && canonicalKey != "X-Tt-Logid" {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(canonicalKey, value)
		}
	}
	if c.Writer.Header().Get("Content-Type") == "" {
		c.Header("Content-Type", "application/json")
	}
	c.Status(response.StatusCode)
	written, writeErr := io.Copy(c.Writer, response.Body)
	if written > 0 {
		c.Set("native_response_committed", true)
	}
	if writeErr != nil {
		if contextErr := c.Request.Context().Err(); contextErr != nil {
			return newNativeASRCancellationError(contextErr)
		}
		options := []types.NewAPIErrorOptions{}
		if c.GetBool("native_response_committed") {
			options = append(options, types.ErrOptionWithSkipRetry())
		}
		return types.NewErrorWithStatusCode(fmt.Errorf("write volcengine native asr response: %w", writeErr), types.ErrorCodeBadResponse, http.StatusBadGateway, options...)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return types.NewErrorWithStatusCode(fmt.Errorf("volcengine native asr http status %d", response.StatusCode), types.ErrorCodeBadResponseStatusCode, response.StatusCode, types.ErrOptionWithSkipRetry())
	}
	return nil
}

func newNativeASRCancellationError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		fmt.Errorf("volcengine native asr request canceled: %w", err),
		types.ErrorCodeBadResponse,
		499,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}
