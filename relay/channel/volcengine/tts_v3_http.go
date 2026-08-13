package volcengine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	v3HTTPDialTimeout           = 15 * time.Second
	v3HTTPTLSHandshakeTimeout   = 10 * time.Second
	v3HTTPResponseHeaderTimeout = 30 * time.Second
	v3HTTPInitialLineBuffer     = 64 << 10
	v3HTTPMaxLineSize           = 16 << 20
	v3HTTPCompleteCode          = 20000000
	v3MaxErrorBodySize          = 4096
)

type v3HTTPRequestBody struct {
	User      *v3UserMeta      `json:"user,omitempty"`
	Namespace string           `json:"namespace"`
	ReqParams v3StartReqParams `json:"req_params"`
}

type v3HTTPStreamResponse struct {
	Code    int           `json:"code"`
	Message string        `json:"message,omitempty"`
	Data    string        `json:"data,omitempty"`
	Usage   *v3UsageStats `json:"usage,omitempty"`
}

func handleTTSV3HTTPChunked(c *gin.Context, requestURL string, request VolcengineTTSRequest, info *relaycommon.RelayInfo, encoding string, cfg dto.VolcTTSConfig) (any, *types.NewAPIError) {
	startSession := buildV3StartSession(request, encoding)
	body, err := common.Marshal(v3HTTPRequestBody{
		User:      startSession.User,
		Namespace: startSession.Namespace,
		ReqParams: startSession.ReqParams,
	})
	if err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("marshal volcengine v3 http request: %w", err), types.ErrorCodeBadRequestBody, http.StatusInternalServerError)
	}

	headers, err := buildV3AuthHeaders(info.ApiKey, cfg, uuid.NewString())
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeChannelInvalidKey, http.StatusUnauthorized)
	}
	headers.Set("Content-Type", "application/json")
	diagnosticBody, diagnosticFlow := service.PrepareDiagnosticOutboundRequest(c, info, http.MethodPost, requestURL, headers, bytes.NewReader(body))
	upstreamRequest, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, requestURL, diagnosticBody)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("build volcengine v3 http request: %w", err), types.ErrorCodeBadRequestBody, http.StatusInternalServerError)
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
		if c.Request.Context().Err() != nil {
			return nil, nil
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("volcengine v3 http request failed: %w", err), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	defer response.Body.Close()
	service.RecordDiagnosticOutboundResponseMetadata(diagnosticFlow, response.StatusCode, response.Header)
	if logID := response.Header.Get("X-Tt-Logid"); logID != "" {
		c.Header("X-Volc-Logid", logID)
	}

	if response.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(io.LimitReader(response.Body, v3MaxErrorBodySize))
		detail := redactV3CredentialText(string(errorBody), info.ApiKey)
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("volcengine v3 http status %d: %s", response.StatusCode, detail), types.ErrorCodeBadResponseStatusCode, response.StatusCode)
	}

	wroteAudio := false
	var usageStats *v3UsageStats
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, v3HTTPInitialLineBuffer), v3HTTPMaxLineSize)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		message := v3HTTPStreamResponse{}
		if err = common.Unmarshal(line, &message); err != nil {
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("parse volcengine v3 http response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		if message.Usage != nil {
			usageStats = message.Usage
		}
		if message.Code == v3HTTPCompleteCode {
			break
		}
		if message.Code != 0 {
			detail := message.Message
			if len(detail) > v3MaxErrorBodySize {
				detail = detail[:v3MaxErrorBodySize]
			}
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("volcengine v3 http provider error: code=%d message=%s", message.Code, detail), types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		if message.Data == "" {
			continue
		}

		audio, decodeErr := base64.StdEncoding.DecodeString(message.Data)
		if decodeErr != nil {
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("decode volcengine v3 http audio: %w", decodeErr), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		if len(audio) == 0 {
			continue
		}
		if !wroteAudio {
			c.Header("Content-Type", getContentTypeByEncoding(encoding))
			c.Header("Transfer-Encoding", "chunked")
		}
		if _, err = c.Writer.Write(audio); err != nil {
			if c.Request.Context().Err() != nil {
				return nil, nil
			}
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("write volcengine v3 http audio: %w", err), types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		c.Writer.Flush()
		wroteAudio = true
	}
	if err = scanner.Err(); err != nil {
		if c.Request.Context().Err() != nil {
			return nil, nil
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("read volcengine v3 http response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if !wroteAudio {
		return nil, types.NewErrorWithStatusCode(errors.New("volcengine v3 http stream ended without audio"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	usage, clamp, err := parseV3UsageStatsChecked(usageStats, info.GetEstimatePromptTokens())
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	info.QuotaClamp = clamp
	return usage, nil
}

func newV3StreamingHTTPClient(proxyURL string) (*http.Client, error) {
	baseClient, err := service.GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, err
	}
	baseTransport := baseClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	transport, ok := baseTransport.(*http.Transport)
	if !ok || transport == nil {
		return nil, errors.New("volcengine v3 streaming requires an HTTP transport")
	}
	transport = transport.Clone()
	originalDial := transport.DialContext
	if originalDial == nil {
		originalDial = (&net.Dialer{Timeout: v3HTTPDialTimeout, KeepAlive: 30 * time.Second}).DialContext
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialContext, cancel := context.WithTimeout(ctx, v3HTTPDialTimeout)
		defer cancel()
		return originalDial(dialContext, network, address)
	}
	transport.TLSHandshakeTimeout = v3HTTPTLSHandshakeTimeout
	transport.ResponseHeaderTimeout = v3HTTPResponseHeaderTimeout

	return &http.Client{
		Transport:     transport,
		CheckRedirect: baseClient.CheckRedirect,
		Timeout:       0,
	}, nil
}

func redactV3CredentialText(text, apiKey string) string {
	redacted := strings.ReplaceAll(text, apiKey, "[REDACTED]")
	for _, part := range strings.Split(apiKey, "|") {
		part = strings.TrimSpace(part)
		if part != "" {
			redacted = strings.ReplaceAll(redacted, part, "[REDACTED]")
		}
	}
	return redacted
}
