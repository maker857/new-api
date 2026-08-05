package service

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type diagnosticSSEEvent struct {
	name string
	data []byte
}

func diagnosticCapturePartIsSSE(state *diagnosticCapturePartState) bool {
	if state == nil || state.part != "response" || state.meta == nil {
		return false
	}
	headers, ok := state.meta["headers"].(map[string][]string)
	if !ok {
		return false
	}
	for name, values := range headers {
		if !strings.EqualFold(name, "Content-Type") {
			continue
		}
		for _, value := range values {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "text/event-stream") {
				return true
			}
		}
	}
	return false
}

// diagnosticCaptureSSEFile converts a completed Anthropic Messages SSE response
// into the equivalent message object. The original text is returned only when
// conversion fails so callers can preserve a readable diagnostic fallback.
func diagnosticCaptureSSEFile(path string) ([]byte, []byte, string, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", 0, err
	}
	events, err := diagnosticParseSSE(raw)
	if err != nil {
		return nil, raw, err.Error(), len(events), nil
	}
	message, err := diagnosticAggregateAnthropicSSE(events)
	if err != nil {
		return nil, raw, err.Error(), len(events), nil
	}
	data, err := common.Marshal(message)
	if err != nil {
		return nil, raw, err.Error(), len(events), nil
	}
	return data, nil, "", len(events), nil
}

func diagnosticParseSSE(raw []byte) ([]diagnosticSSEEvent, error) {
	reader := bufio.NewReader(bytes.NewReader(raw))
	events := make([]diagnosticSSEEvent, 0, 32)
	name := "message"
	data := make([][]byte, 0, 1)
	appendEvent := func() {
		if len(data) == 0 {
			name = "message"
			return
		}
		events = append(events, diagnosticSSEEvent{name: name, data: bytes.Join(data, []byte("\n"))})
		name = "message"
		data = data[:0]
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			if line == "" {
				appendEvent()
			} else if !strings.HasPrefix(line, ":") {
				switch {
				case strings.HasPrefix(line, "event:"):
					name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				case strings.HasPrefix(line, "data:"):
					value := strings.TrimPrefix(line, "data:")
					if strings.HasPrefix(value, " ") {
						value = value[1:]
					}
					data = append(data, []byte(value))
				}
			}
		}
		if err == io.EOF {
			appendEvent()
			return events, nil
		}
		if err != nil {
			return events, err
		}
	}
}

func diagnosticAggregateAnthropicSSE(events []diagnosticSSEEvent) (map[string]any, error) {
	var message map[string]any
	blocks := make(map[int]map[string]any)
	toolInputs := make(map[int]string)
	stopped := false

	for _, event := range events {
		if len(event.data) == 0 || bytes.Equal(event.data, []byte("[DONE]")) {
			continue
		}
		var payload map[string]any
		if err := common.Unmarshal(event.data, &payload); err != nil {
			return nil, fmt.Errorf("invalid SSE JSON in %s: %w", event.name, err)
		}
		eventType, _ := payload["type"].(string)
		if eventType == "" {
			eventType = event.name
		}

		switch eventType {
		case "message_start":
			started, ok := payload["message"].(map[string]any)
			if !ok {
				return nil, errors.New("message_start has no message object")
			}
			message = started
			if _, ok := message["content"].([]any); !ok {
				message["content"] = []any{}
			}
		case "content_block_start":
			if message == nil {
				return nil, errors.New("content block arrived before message_start")
			}
			index, err := diagnosticSSEIndex(payload)
			if err != nil {
				return nil, err
			}
			block, ok := payload["content_block"].(map[string]any)
			if !ok {
				return nil, errors.New("content_block_start has no content block")
			}
			blocks[index] = block
			content, _ := message["content"].([]any)
			for len(content) <= index {
				content = append(content, nil)
			}
			content[index] = block
			message["content"] = content
		case "content_block_delta":
			index, err := diagnosticSSEIndex(payload)
			if err != nil {
				return nil, err
			}
			block := blocks[index]
			if block == nil {
				return nil, fmt.Errorf("content delta has no block at index %d", index)
			}
			delta, ok := payload["delta"].(map[string]any)
			if !ok {
				return nil, errors.New("content_block_delta has no delta object")
			}
			switch delta["type"] {
			case "text_delta":
				text, _ := block["text"].(string)
				deltaText, _ := delta["text"].(string)
				block["text"] = text + deltaText
			case "thinking_delta":
				thinking, _ := block["thinking"].(string)
				deltaThinking, _ := delta["thinking"].(string)
				block["thinking"] = thinking + deltaThinking
			case "signature_delta":
				signature, _ := block["signature"].(string)
				deltaSignature, _ := delta["signature"].(string)
				block["signature"] = signature + deltaSignature
			case "input_json_delta":
				partialJSON, _ := delta["partial_json"].(string)
				toolInputs[index] += partialJSON
			default:
				return nil, fmt.Errorf("unsupported content block delta type %q", delta["type"])
			}
		case "content_block_stop":
			index, err := diagnosticSSEIndex(payload)
			if err != nil {
				return nil, err
			}
			if input, ok := toolInputs[index]; ok {
				if blocks[index] == nil {
					return nil, fmt.Errorf("tool input has no block at index %d", index)
				}
				var parsedInput any
				if err := common.Unmarshal([]byte(input), &parsedInput); err != nil {
					return nil, fmt.Errorf("invalid tool input at index %d: %w", index, err)
				}
				blocks[index]["input"] = parsedInput
			}
		case "message_delta":
			if message == nil {
				return nil, errors.New("message_delta arrived before message_start")
			}
			if delta, ok := payload["delta"].(map[string]any); ok {
				for key, value := range delta {
					message[key] = value
				}
			}
			if usage, ok := payload["usage"].(map[string]any); ok {
				mergedUsage, _ := message["usage"].(map[string]any)
				if mergedUsage == nil {
					mergedUsage = make(map[string]any)
				}
				for key, value := range usage {
					mergedUsage[key] = value
				}
				message["usage"] = mergedUsage
			}
			if contextManagement, ok := payload["context_management"]; ok {
				message["context_management"] = contextManagement
			}
		case "message_stop":
			stopped = true
		}
	}

	if message == nil {
		return nil, errors.New("no Anthropic message_start event")
	}
	if !stopped {
		return nil, errors.New("SSE stream ended before message_stop")
	}
	return message, nil
}

func diagnosticSSEIndex(payload map[string]any) (int, error) {
	value, ok := payload["index"].(float64)
	if !ok || value < 0 || value != float64(int(value)) {
		return 0, errors.New("SSE event has invalid content block index")
	}
	return int(value), nil
}
