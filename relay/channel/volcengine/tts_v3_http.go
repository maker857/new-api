package volcengine

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	v3HTTPDialTimeout           = 15 * time.Second
	v3HTTPTLSHandshakeTimeout   = 10 * time.Second
	v3HTTPResponseHeaderTimeout = 30 * time.Second
	v3MaxFrameHeaderSize        = 64
	v3MaxIdentifierSize         = 64 << 10
	v3MaxPayloadSize            = 16 << 20
	v3MaxErrorBodySize          = 4096
)

type v3HTTPRequestBody struct {
	User      *v3UserMeta      `json:"user,omitempty"`
	Namespace string           `json:"namespace"`
	ReqParams v3StartReqParams `json:"req_params"`
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

	c.Header("Content-Type", getContentTypeByEncoding(encoding))
	c.Header("Transfer-Encoding", "chunked")
	wroteAudio := false
	var usage *dto.Usage
	for {
		message, readErr := ReadOneV3Frame(response.Body)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			if c.Request.Context().Err() != nil {
				return nil, nil
			}
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("parse volcengine v3 http frame: %w", readErr), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		switch message.MsgType {
		case MsgTypeError:
			return nil, v3ProviderMessageError(message)
		case MsgTypeAudioOnlyServer:
			if message.EventType == EventType_TTSResponse && len(message.Payload) > 0 {
				if _, err = c.Writer.Write(message.Payload); err != nil {
					if c.Request.Context().Err() != nil {
						return nil, nil
					}
					return nil, types.NewErrorWithStatusCode(fmt.Errorf("write volcengine v3 http audio: %w", err), types.ErrorCodeBadResponse, http.StatusBadGateway)
				}
				c.Writer.Flush()
				wroteAudio = true
			}
		case MsgTypeFullServerResponse:
			switch message.EventType {
			case EventType_SessionFinished:
				usage, info.QuotaClamp, err = parseV3UsageChecked(message.Payload, info.GetEstimatePromptTokens())
				if err != nil {
					return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				}
				if !wroteAudio {
					return nil, types.NewErrorWithStatusCode(errors.New("volcengine v3 http completed without audio"), types.ErrorCodeBadResponse, http.StatusBadGateway)
				}
				c.Status(http.StatusOK)
				return usage, nil
			case EventType_SessionFailed, EventType_ConnectionFailed:
				return nil, v3ProviderMessageError(message)
			}
		}
	}
	if !wroteAudio {
		return nil, types.NewErrorWithStatusCode(errors.New("volcengine v3 http stream ended without audio"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	usage, info.QuotaClamp, err = parseV3UsageChecked(nil, info.GetEstimatePromptTokens())
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
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

func ReadOneV3Frame(reader io.Reader) (*Message, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	headerSize := int(header[0]&0x0f) * 4
	if headerSize < 4 || headerSize > v3MaxFrameHeaderSize {
		return nil, fmt.Errorf("invalid volcengine v3 frame header size: %d", headerSize)
	}
	frame := bytes.NewBuffer(header)
	if err := copyV3FrameBytes(reader, frame, headerSize-4); err != nil {
		return nil, err
	}

	messageType := MsgType(header[1] >> 4)
	flag := MsgTypeFlagBits(header[1] & 0x0f)
	if flag == MsgTypeFlagWithEvent {
		eventBytes := make([]byte, 4)
		if _, err := io.ReadFull(reader, eventBytes); err != nil {
			return nil, err
		}
		frame.Write(eventBytes)
		event := EventType(int32(binary.BigEndian.Uint32(eventBytes)))
		if !isV3ConnectionEvent(event) {
			if err := copyV3LengthPrefixed(reader, frame, v3MaxIdentifierSize); err != nil {
				return nil, err
			}
		}
		if isV3ConnectionResponseEvent(event) {
			if err := copyV3LengthPrefixed(reader, frame, v3MaxIdentifierSize); err != nil {
				return nil, err
			}
		}
	}

	switch messageType {
	case MsgTypeFullClientRequest, MsgTypeFullServerResponse, MsgTypeFrontEndResultServer, MsgTypeAudioOnlyClient, MsgTypeAudioOnlyServer:
		if flag == MsgTypeFlagPositiveSeq || flag == MsgTypeFlagNegativeSeq {
			if err := copyV3FrameBytes(reader, frame, 4); err != nil {
				return nil, err
			}
		}
	case MsgTypeError:
		if err := copyV3FrameBytes(reader, frame, 4); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported volcengine v3 message type: %d", messageType)
	}
	if err := copyV3LengthPrefixed(reader, frame, v3MaxPayloadSize); err != nil {
		return nil, err
	}
	return ParseFrame(frame.Bytes())
}

func copyV3FrameBytes(reader io.Reader, frame *bytes.Buffer, size int) error {
	if size == 0 {
		return nil
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return err
	}
	_, _ = frame.Write(data)
	return nil
}

func copyV3LengthPrefixed(reader io.Reader, frame *bytes.Buffer, maximum uint32) error {
	sizeBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, sizeBytes); err != nil {
		return err
	}
	_, _ = frame.Write(sizeBytes)
	size := binary.BigEndian.Uint32(sizeBytes)
	if size > maximum {
		return fmt.Errorf("volcengine v3 frame field exceeds limit: %d > %d", size, maximum)
	}
	return copyV3FrameBytes(reader, frame, int(size))
}

func isV3ConnectionEvent(event EventType) bool {
	switch event {
	case EventType_StartConnection, EventType_FinishConnection, EventType_ConnectionStarted, EventType_ConnectionFailed, EventType_ConnectionFinished:
		return true
	default:
		return false
	}
}

func isV3ConnectionResponseEvent(event EventType) bool {
	switch event {
	case EventType_ConnectionStarted, EventType_ConnectionFailed, EventType_ConnectionFinished:
		return true
	default:
		return false
	}
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
