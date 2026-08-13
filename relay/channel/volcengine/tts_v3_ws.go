package volcengine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	v3WSHandshakeTimeout = 15 * time.Second
	v3WSFrameIdleTimeout = 30 * time.Second
	v3WSReadLimit        = 16 << 20
)

func handleTTSV3WSUnidirectional(c *gin.Context, requestURL string, request VolcengineTTSRequest, info *relaycommon.RelayInfo, encoding string, cfg dto.VolcTTSConfig) (any, *types.NewAPIError) {
	connectID := uuid.NewString()
	headers, err := buildV3AuthHeaders(info.ApiKey, cfg, connectID)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeChannelInvalidKey, http.StatusUnauthorized)
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = v3WSHandshakeTimeout
	client, clientErr := service.GetHttpClientWithProxy(info.ChannelSetting.Proxy)
	if clientErr != nil {
		return nil, types.NewErrorWithStatusCode(clientErr, types.ErrorCodeBadRequestBody, http.StatusBadRequest)
	}
	if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
		dialer.Proxy = transport.Proxy
		dialer.NetDialContext = transport.DialContext
		if transport.TLSClientConfig != nil {
			dialer.TLSClientConfig = transport.TLSClientConfig.Clone()
		}
	}

	_, diagnosticFlow := service.PrepareDiagnosticOutboundRequest(c, info, http.MethodGet, requestURL, headers, nil)
	conn, response, dialErr := dialer.DialContext(c.Request.Context(), requestURL, headers)
	if dialErr != nil {
		service.RecordDiagnosticOutboundFailure(diagnosticFlow, dialErr)
		if c.Request.Context().Err() != nil {
			return nil, nil
		}
		status := http.StatusBadGateway
		logID := ""
		if response != nil {
			status = response.StatusCode
			logID = response.Header.Get("X-Tt-Logid")
			if response.Body != nil {
				_ = response.Body.Close()
			}
		}
		if logID != "" {
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("volcengine v3 websocket handshake failed: status=%d logid=%s", status, logID), types.ErrorCodeBadResponseStatusCode, status)
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("volcengine v3 websocket handshake failed: status=%d: %w", status, dialErr), types.ErrorCodeBadResponseStatusCode, status)
	}
	defer conn.Close()
	conn.SetReadLimit(v3WSReadLimit)
	if response != nil {
		service.RecordDiagnosticOutboundResponseMetadata(diagnosticFlow, response.StatusCode, response.Header)
		if logID := response.Header.Get("X-Tt-Logid"); logID != "" {
			c.Header("X-Volc-Logid", logID)
		}
		if response.Body != nil {
			_ = response.Body.Close()
		}
	} else {
		service.RecordDiagnosticOutboundResponseMetadata(diagnosticFlow, http.StatusSwitchingProtocols, nil)
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-c.Request.Context().Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	if apiErr := sendV3WSEvent(c, conn, requestURL, EventType_StartConnection, "", nil); apiErr != nil {
		return nil, apiErr
	}
	if apiErr := expectV3WSEvent(c, conn, requestURL, EventType_ConnectionStarted, EventType_ConnectionFailed); apiErr != nil {
		if c.Request.Context().Err() != nil {
			return nil, nil
		}
		return nil, apiErr
	}

	sessionID := uuid.NewString()
	startPayload, err := buildV3StartSessionPayload(request, encoding)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("build volcengine v3 start session: %w", err), types.ErrorCodeBadRequestBody, http.StatusInternalServerError)
	}
	if apiErr := sendV3WSEvent(c, conn, requestURL, EventType_StartSession, sessionID, startPayload); apiErr != nil {
		return nil, apiErr
	}
	if apiErr := expectV3WSEvent(c, conn, requestURL, EventType_SessionStarted, EventType_SessionFailed); apiErr != nil {
		if c.Request.Context().Err() != nil {
			return nil, nil
		}
		return nil, apiErr
	}
	if apiErr := sendV3WSEvent(c, conn, requestURL, EventType_FinishSession, sessionID, nil); apiErr != nil {
		return nil, apiErr
	}

	c.Header("Content-Type", getContentTypeByEncoding(encoding))
	c.Header("Transfer-Encoding", "chunked")
	wroteAudio := false
	var usage *dto.Usage
	for {
		message, apiErr := receiveV3WSMessage(c, conn, requestURL)
		if apiErr != nil {
			if c.Request.Context().Err() != nil {
				return nil, nil
			}
			return nil, apiErr
		}
		if message == nil {
			return nil, nil
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
					return nil, types.NewErrorWithStatusCode(fmt.Errorf("write volcengine v3 audio: %w", err), types.ErrorCodeBadResponse, http.StatusBadGateway)
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
				_ = sendV3WSEvent(c, conn, requestURL, EventType_FinishConnection, "", nil)
				if !wroteAudio {
					return nil, types.NewErrorWithStatusCode(errors.New("volcengine v3 completed without audio"), types.ErrorCodeBadResponse, http.StatusBadGateway)
				}
				c.Status(http.StatusOK)
				return usage, nil
			case EventType_SessionFailed, EventType_ConnectionFailed:
				return nil, v3ProviderMessageError(message)
			}
		}
	}
}

func sendV3WSEvent(c *gin.Context, conn *websocket.Conn, requestURL string, event EventType, sessionID string, payload []byte) *types.NewAPIError {
	message, err := NewEventMessage(event, sessionID, payload)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeBadRequestBody, http.StatusInternalServerError)
	}
	frame, err := message.Marshal()
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeBadRequestBody, http.StatusInternalServerError)
	}
	if err = conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		service.RecordDiagnosticWebSocketFailure(c, requestURL, err)
		if c.Request.Context().Err() != nil {
			return nil
		}
		return types.NewErrorWithStatusCode(fmt.Errorf("send volcengine v3 %s: %w", event, err), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	service.RecordDiagnosticWebSocketFrame(c, requestURL, "request", frame)
	return nil
}

func receiveV3WSMessage(c *gin.Context, conn *websocket.Conn, requestURL string) (*Message, *types.NewAPIError) {
	_ = conn.SetReadDeadline(time.Now().Add(v3WSFrameIdleTimeout))
	message, frame, err := ReceiveMessageFrame(conn)
	if err != nil {
		service.RecordDiagnosticWebSocketFailure(c, requestURL, err)
		if c.Request.Context().Err() != nil || errors.Is(err, context.Canceled) {
			return nil, nil
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("receive volcengine v3 frame: %w", err), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	service.RecordDiagnosticWebSocketFrame(c, requestURL, "response", frame)
	return message, nil
}

func expectV3WSEvent(c *gin.Context, conn *websocket.Conn, requestURL string, success, failure EventType) *types.NewAPIError {
	for {
		message, apiErr := receiveV3WSMessage(c, conn, requestURL)
		if apiErr != nil || message == nil {
			return apiErr
		}
		if message.MsgType == MsgTypeError {
			return v3ProviderMessageError(message)
		}
		if message.EventType == success {
			return nil
		}
		if message.EventType == failure {
			return v3ProviderMessageError(message)
		}
	}
}

func v3ProviderMessageError(message *Message) *types.NewAPIError {
	detail := string(message.Payload)
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	return types.NewErrorWithStatusCode(fmt.Errorf("volcengine v3 provider error: event=%s code=%d body=%s", message.EventType, message.ErrorCode, detail), types.ErrorCodeBadResponse, http.StatusBadGateway)
}
