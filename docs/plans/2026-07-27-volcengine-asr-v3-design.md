# VolcEngine ASR v3 Native Adaptation Design

## Goal

Add native VolcEngine ASR v3 support under the VolcEngine (火山方舟) channel type for audio/video-source transcription with character-level timestamps, while preserving the official VolcEngine request and response formats.

## Scope

- Add native HTTP routes for the official recording-file ASR v3 submit/query workflow:
  - `POST /api/v3/auc/bigmodel/submit`
  - `POST /api/v3/auc/bigmodel/query`
- Authenticate New API clients with the gateway token and replace it with the channel's VolcEngine credentials.
- Select the ASR resource through `X-Api-Resource-Id`; do not reuse TTS or video resource IDs.
- Preserve `utterances`, `words`, `start_time`, `end_time`, and related upstream fields in native responses.
- Add independent ASR channel settings alongside existing VolcEngine TTS settings.
- Keep `/v1/audio/transcriptions` unchanged in this phase; an OpenAI-compatible response bridge is a later phase.

## Architecture

The native ASR routes will use a dedicated relay mode and handler, parallel to the existing native TTS handler. A shared VolcEngine ASR configuration will contain protocol, resource ID, and authentication mode, while the existing `VolcTTSConfig` remains TTS-only. The submit and query payloads will be treated as native JSON and passed through with validation of route, API type, configured resource, and request identifiers.

The upstream endpoints are the VolcEngine v3 recording-file ASR APIs at `https://openspeech.bytedance.com/api/v3/auc/bigmodel/submit` and `/query`. The request must enable `show_utterances` when word-level timestamps are required. The gateway must not convert the native response into the limited Whisper segment schema in this phase.

## Error and billing behavior

- Reject non-VolcEngine API types, missing ASR configuration, and resource mismatches with a non-retryable 4xx error.
- Preserve upstream error status and body before any downstream response bytes are committed.
- Use conservative text/audio usage settlement compatible with the existing relay accounting path; do not derive billing from unbounded upstream duration values.

## Testing

- Relay-mode detection and route registration tests for submit/query.
- Native request parsing tests for required identifiers and resource matching.
- Upstream request tests verifying credential replacement and ASR headers.
- Response passthrough tests proving `utterances.words` and millisecond timestamps are preserved.
- Error tests for invalid API type, missing configuration, resource mismatch, upstream errors, and cancellation.

