# Volcengine Native TTS v3 HTTP Design

## Goal

Expose the Volcengine/Doubao native unidirectional TTS HTTP protocol through New API so clients using the official SDK can switch to the gateway by changing the base URL and credential only. The native response must preserve subtitle and word-level timestamp data that cannot be represented by the existing OpenAI-compatible `/v1/audio/speech` response.

## Scope

This phase adds the native HTTP Chunked endpoint:

```text
POST /api/v3/tts/unidirectional
```

The endpoint mirrors the official upstream path and preserves the native JSON request and newline-delimited JSON response format. Native WebSocket support at `/api/v3/tts/unidirectional/stream` is intentionally deferred.

The existing `/v1/audio/speech` behavior remains unchanged and continues to return raw audio.

## Client Contract

Clients send the same request headers and body shape expected by the Volcengine SDK. For gateway authentication, `X-Api-Key` contains a New API token rather than the upstream Volcengine credential:

```http
POST /api/v3/tts/unidirectional
X-Api-Key: sk-new-api-token
X-Api-Resource-Id: seed-tts-2.0
Content-Type: application/json
```

The request body is the native Volcengine v3 payload, including optional provider fields such as:

```json
{
  "namespace": "UnidirectionalTTS",
  "req_params": {
    "text": "你好",
    "speaker": "your-speaker-id",
    "audio_params": {
      "format": "mp3",
      "enable_subtitle": true
    }
  }
}
```

The gateway returns the upstream newline-delimited JSON records without converting them to raw audio. Records may therefore include `data`, `sentence.words`, `sentence.phonemes`, `usage`, and provider error fields.

## Routing And Authentication

The native endpoint uses the standard New API token, user, group, model permission, channel selection, rate limiting, quota, and logging pipeline.

For this route only, token authentication accepts `X-Api-Key` as the New API credential. The client-supplied value must never be forwarded upstream. Before sending the upstream request, the Volcengine adaptor replaces it with the selected channel's configured credential and applies the channel's configured authentication mode.

`X-Api-Resource-Id` identifies the requested gateway model for channel selection. The initial supported resource is `seed-tts-2.0`, which is the resource documented to support `audio_params.enable_subtitle`. Existing model aliases and channel model mapping remain applicable.

The selected channel configuration is authoritative for the upstream resource ID and credential. A client cannot use request headers to bypass channel configuration or access an unconfigured upstream resource.

Request IDs and connection IDs are forwarded when safe and generated when absent, following Volcengine's required header contract.

## Request Forwarding

The gateway parses only the fields needed for validation, routing, token estimation, and billing. The full request remains available as structured JSON so supported native fields can be forwarded without maintaining a duplicate exhaustive DTO.

The adaptor must:

1. Validate that the body is JSON and contains non-empty `req_params.text` and `req_params.speaker`.
2. Enforce the project's request-size and billing-safety limits before contacting the provider.
3. Preserve optional native fields, including explicit zero and false values.
4. Replace authentication and resource headers with selected-channel values.
5. Use `common.Marshal`, `common.Unmarshal`, and related wrappers for JSON operations.

Unknown native fields are forwarded so future Volcengine additions do not require immediate gateway changes, provided they do not bypass validation or billing controls.

## Response Streaming

The upstream HTTP response is consumed line by line and written downstream as newline-delimited JSON. Each complete upstream record is forwarded once and flushed immediately.

The gateway may parse a copy of each record for internal accounting, but it must not remove or rename native response fields. In particular, when `enable_subtitle` is enabled, clients must receive the original `sentence.words` and `sentence.phonemes` structures with their timestamps.

The downstream response uses the upstream-compatible content type and HTTP Chunked transfer behavior. Client cancellation cancels the upstream request promptly.

Malformed upstream records, transport failures, and premature stream termination are reported through the established relay error path when no response has been committed. After streaming begins, the connection is terminated and the failure is recorded because the HTTP status can no longer be changed safely.

Provider non-2xx responses are returned with their native JSON body where possible while retaining New API request correlation headers.

## Billing And Usage

Pre-consume uses the validated input text length and the existing TTS pricing path. Final settlement uses the upstream `usage.text_words` value when present, with the same checked quota conversion and saturation auditing used by the existing Volcengine v3 TTS implementation.

The response record containing `usage` is still forwarded unchanged. Parsing usage for billing must not consume or alter the downstream stream.

No request field may introduce an unbounded billing multiplier through passthrough data. Any native field later used in quota calculation must be validated at the adaptor boundary before it affects pre-consume or settlement.

## Diagnostics And Security

Diagnostic capture records the native outbound request and every upstream response record using the existing capture facilities. Credentials are redacted in headers and error text.

The implementation must never:

- Forward the caller's New API token to Volcengine.
- Accept a caller-supplied upstream API key as a channel credential.
- Log raw credentials.
- Allow native headers to override the selected channel's resource ID or authentication mode.

Existing SSRF, proxy, timeout, cancellation, and response-size protections continue to apply.

## Testing

Implementation follows test-driven development. Regression coverage must verify:

- `X-Api-Key` authenticates the native route as a New API token.
- The client token is replaced by the selected channel credential upstream.
- Native request fields, including `enable_subtitle: true` and explicit false/zero values, are preserved.
- Word and phoneme timestamp records are returned unchanged and in order.
- Audio `data` chunks are not decoded or concatenated on the native route.
- Usage is parsed for settlement while the original usage record is still forwarded.
- Client cancellation stops the upstream request.
- Provider errors and malformed stream records follow the defined error behavior.
- `/v1/audio/speech` remains unchanged.

Fresh verification must include targeted relay tests, `go test ./... -count=1`, `go build ./...`, frontend type checking, and the frontend production build when shared frontend contracts are affected.

## Deferred Work

- Native WebSocket endpoint `/api/v3/tts/unidirectional/stream`.
- Bidirectional TTS protocols.
- A provider-neutral timestamp schema for `/v1/audio/speech`.
- Native subtitle support for resources not documented by Volcengine as supporting `enable_subtitle`.
