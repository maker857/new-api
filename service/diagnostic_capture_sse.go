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
	switch headers := state.meta["headers"].(type) {
	case map[string][]string:
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
	case map[string]string:
		for name, value := range headers {
			if strings.EqualFold(name, "Content-Type") && strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "text/event-stream") {
				return true
			}
		}
	}
	return false
}

// diagnosticCaptureSSEFile converts known SSE protocols into a readable JSON
// object. Unknown SSE protocols are retained as an event array so the original
// event payloads remain inspectable instead of being encoded as base64.
func diagnosticCaptureSSEFile(path string) ([]byte, []byte, string, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", 0, err
	}
	events, err := diagnosticParseSSE(raw)
	if err != nil {
		return nil, raw, err.Error(), len(events), nil
	}
	message := diagnosticAggregateSSE(events)
	data, err := common.Marshal(message)
	if err != nil {
		return nil, raw, err.Error(), len(events), nil
	}
	return data, nil, "", len(events), nil
}

func diagnosticAggregateSSE(events []diagnosticSSEEvent) any {
	parsed := make([]map[string]any, 0, len(events))
	for _, event := range events {
		if len(event.data) == 0 {
			continue
		}
		if bytes.Equal(event.data, []byte("[DONE]")) {
			parsed = append(parsed, map[string]any{"event": event.name, "data": "[DONE]"})
			continue
		}
		var payload any
		if common.Unmarshal(event.data, &payload) == nil {
			parsed = append(parsed, map[string]any{"event": event.name, "data": payload})
		} else {
			parsed = append(parsed, map[string]any{"event": event.name, "data": string(event.data)})
		}
	}

	if hasSSEEventType(parsed, "message_start") {
		if message, err := diagnosticAggregateAnthropicSSE(events); err == nil {
			return message
		}
	}
	if hasSSEEventPrefix(parsed, "response.") {
		if response, err := diagnosticAggregateOpenAIResponsesSSE(events); err == nil {
			return response
		}
	}
	if hasChatCompletionChunk(parsed) {
		if response, err := diagnosticAggregateOpenAIChatSSE(events); err == nil {
			return response
		}
	}
	return map[string]any{"events": parsed}
}

func hasSSEEventType(events []map[string]any, name string) bool {
	for _, event := range events {
		if event["event"] == name {
			return true
		}
		if payload, ok := event["data"].(map[string]any); ok && payload["type"] == name {
			return true
		}
	}
	return false
}

func hasSSEEventPrefix(events []map[string]any, prefix string) bool {
	for _, event := range events {
		name, _ := event["event"].(string)
		if strings.HasPrefix(name, prefix) {
			return true
		}
		if payload, ok := event["data"].(map[string]any); ok {
			typeName, _ := payload["type"].(string)
			if strings.HasPrefix(typeName, prefix) {
				return true
			}
		}
	}
	return false
}

func hasChatCompletionChunk(events []map[string]any) bool {
	for _, event := range events {
		if payload, ok := event["data"].(map[string]any); ok {
			if _, ok := payload["choices"]; ok {
				return true
			}
		}
	}
	return false
}

func diagnosticSSEPayload(event diagnosticSSEEvent) (map[string]any, bool) {
	if len(event.data) == 0 || bytes.Equal(event.data, []byte("[DONE]")) {
		return nil, false
	}
	var payload map[string]any
	if common.Unmarshal(event.data, &payload) != nil {
		return nil, false
	}
	return payload, true
}

func diagnosticAggregateOpenAIResponsesSSE(events []diagnosticSSEEvent) (map[string]any, error) {
	var response map[string]any
	for _, event := range events {
		payload, ok := diagnosticSSEPayload(event)
		if !ok {
			continue
		}
		typeName, _ := payload["type"].(string)
		if completed, ok := payload["response"].(map[string]any); ok && (typeName == "response.created" || typeName == "response.completed" || typeName == "response.incomplete" || typeName == "response.failed") {
			response = completed
			continue
		}
		if item, ok := payload["item"].(map[string]any); ok && (typeName == "response.output_item.added" || typeName == "response.output_item.done") {
			if response == nil {
				response = map[string]any{"object": "response", "output": []any{}}
			}
			output, _ := response["output"].([]any)
			index := diagnosticSSENumber(payload["output_index"])
			for len(output) <= index {
				output = append(output, nil)
			}
			output[index] = item
			response["output"] = output
		}
	}
	if response == nil {
		return nil, errors.New("no OpenAI Responses payload")
	}
	return response, nil
}

func diagnosticAggregateOpenAIChatSSE(events []diagnosticSSEEvent) (map[string]any, error) {
	response := map[string]any{"object": "chat.completion", "choices": []any{}}
	choices := make(map[int]map[string]any)
	for _, event := range events {
		payload, ok := diagnosticSSEPayload(event)
		if !ok {
			continue
		}
		for _, rawChoice := range diagnosticSSEArray(payload["choices"]) {
			choice, ok := rawChoice.(map[string]any)
			if !ok {
				continue
			}
			index := diagnosticSSENumber(choice["index"])
			result := choices[index]
			if result == nil {
				result = map[string]any{"index": index, "message": map[string]any{"role": "assistant", "content": ""}}
				choices[index] = result
			}
			if value, ok := payload["id"]; ok {
				response["id"] = value
			}
			if value, ok := payload["model"]; ok {
				response["model"] = value
			}
			if delta, ok := choice["delta"].(map[string]any); ok {
				message := result["message"].(map[string]any)
				if role, ok := delta["role"]; ok {
					message["role"] = role
				}
				if text, ok := delta["content"].(string); ok {
					message["content"] = message["content"].(string) + text
				}
			}
			if finish, ok := choice["finish_reason"]; ok {
				result["finish_reason"] = finish
			}
		}
		if usage, ok := payload["usage"]; ok {
			response["usage"] = usage
		}
	}
	output := make([]any, 0, len(choices))
	for i := 0; i <= len(choices); i++ {
		if choice, ok := choices[i]; ok {
			output = append(output, choice)
		}
	}
	if len(output) == 0 {
		return nil, errors.New("no OpenAI Chat Completion choices")
	}
	response["choices"] = output
	return response, nil
}

func diagnosticSSENumber(value any) int {
	if number, ok := value.(float64); ok && number >= 0 {
		return int(number)
	}
	if number, ok := value.(int); ok && number >= 0 {
		return number
	}
	return 0
}

func diagnosticSSEArray(value any) []any {
	items, _ := value.([]any)
	return items
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
