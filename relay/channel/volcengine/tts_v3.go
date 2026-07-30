package volcengine

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	volcTTSV3WSUnidirectionalEndpoint = "wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream"
	volcTTSV3HTTPChunkedEndpoint      = "https://openspeech.bytedance.com/api/v3/tts/unidirectional"
)

type v3StartSessionPayload struct {
	User      *v3UserMeta      `json:"user,omitempty"`
	Event     int32            `json:"event"`
	Namespace string           `json:"namespace"`
	ReqParams v3StartReqParams `json:"req_params"`
}

type v3UserMeta struct {
	UID string `json:"uid,omitempty"`
}

type v3StartReqParams struct {
	Text        string        `json:"text"`
	Speaker     string        `json:"speaker"`
	AudioParams v3AudioParams `json:"audio_params"`
}

type v3AudioParams struct {
	Format     string `json:"format,omitempty"`
	SampleRate *int   `json:"sample_rate,omitempty"`
	BitRate    *int   `json:"bit_rate,omitempty"`
	SpeechRate *int   `json:"speech_rate,omitempty"`
}

type v3SessionResultEnvelope struct {
	StatusCode int           `json:"status_code,omitempty"`
	Message    string        `json:"message,omitempty"`
	Usage      *v3UsageStats `json:"usage,omitempty"`
}

type v3UsageStats struct {
	TextWords float64 `json:"text_words"`
}

func buildV3AuthHeaders(apiKey string, cfg dto.VolcTTSConfig, connectID string) (http.Header, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("volcengine tts api key is required")
	}
	connectID = strings.TrimSpace(connectID)
	if connectID == "" {
		return nil, errors.New("volcengine tts connect id is required")
	}

	headers := make(http.Header)
	switch cfg.EffectiveAuthMode() {
	case dto.VolcTTSAuthModeNewConsole:
		if strings.Contains(apiKey, "|") {
			return nil, errors.New("invalid volcengine tts api key format for new_console auth")
		}
		headers.Set("X-Api-Key", apiKey)
	case dto.VolcTTSAuthModeLegacy:
		parts := strings.Split(apiKey, "|")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, errors.New("invalid volcengine tts api key format for legacy auth")
		}
		headers.Set("X-Api-App-Id", strings.TrimSpace(parts[0]))
		headers.Set("X-Api-Access-Key", strings.TrimSpace(parts[1]))
	default:
		return nil, fmt.Errorf("unsupported volcengine tts auth mode: %s", cfg.AuthMode)
	}

	headers.Set("X-Api-Resource-Id", cfg.ResourceID)
	headers.Set("X-Api-Connect-Id", connectID)
	if cfg.ShouldRequireUsage() {
		headers.Set("X-Control-Require-Usage-Tokens-Return", "*")
	}
	return headers, nil
}

func buildV3StartSessionPayload(request VolcengineTTSRequest, encoding string) ([]byte, error) {
	payload := buildV3StartSession(request, encoding)
	return common.Marshal(payload)
}

func buildV3StartSession(request VolcengineTTSRequest, encoding string) v3StartSessionPayload {
	audioParams := v3AudioParams{Format: encoding}
	if request.Audio.Rate != 0 {
		rate := request.Audio.Rate
		audioParams.SampleRate = &rate
	}
	if request.Audio.Bitrate != 0 {
		bitrate := request.Audio.Bitrate
		audioParams.BitRate = &bitrate
	}
	if request.Audio.SpeedRatio != 0 {
		speechRate := int(math.Round((request.Audio.SpeedRatio - 1) * 100))
		if speechRate < -50 {
			speechRate = -50
		}
		if speechRate > 100 {
			speechRate = 100
		}
		audioParams.SpeechRate = &speechRate
	}

	return v3StartSessionPayload{
		User:      &v3UserMeta{UID: request.User.UID},
		Event:     int32(EventType_StartSession),
		Namespace: "UnidirectionalTTS",
		ReqParams: v3StartReqParams{
			Text:        request.Request.Text,
			Speaker:     request.Audio.VoiceType,
			AudioParams: audioParams,
		},
	}
}

func parseV3Usage(payload []byte, fallback int) (*dto.Usage, error) {
	usage, _, err := parseV3UsageChecked(payload, fallback)
	return usage, err
}

func parseV3UsageChecked(payload []byte, fallback int) (*dto.Usage, *common.QuotaClamp, error) {
	envelope := v3SessionResultEnvelope{}
	if len(payload) > 0 {
		if err := common.Unmarshal(payload, &envelope); err != nil {
			return nil, nil, fmt.Errorf("parse volcengine v3 usage: %w", err)
		}
	}
	return parseV3UsageStatsChecked(envelope.Usage, fallback)
}

func parseV3UsageStatsChecked(stats *v3UsageStats, fallback int) (*dto.Usage, *common.QuotaClamp, error) {
	words := fallback
	var clamp *common.QuotaClamp
	if stats != nil && stats.TextWords > 0 {
		words, clamp = common.QuotaFromFloatChecked(stats.TextWords)
	}
	if words < 0 {
		words = 0
	}
	return &dto.Usage{
		PromptTokens: words,
		TotalTokens:  words,
	}, clamp, nil
}

func getV3TTSEndpoint(protocol string) (string, error) {
	switch strings.TrimSpace(protocol) {
	case dto.VolcTTSProtocolV3WsUni:
		return volcTTSV3WSUnidirectionalEndpoint, nil
	case dto.VolcTTSProtocolV3HTTPChunked:
		return volcTTSV3HTTPChunkedEndpoint, nil
	default:
		return "", fmt.Errorf("unsupported volcengine tts v3 protocol: %s", protocol)
	}
}

func parseV3SessionResult(payload []byte) (*v3SessionResultEnvelope, error) {
	envelope := &v3SessionResultEnvelope{}
	if err := common.Unmarshal(payload, envelope); err != nil {
		return nil, err
	}
	return envelope, nil
}
