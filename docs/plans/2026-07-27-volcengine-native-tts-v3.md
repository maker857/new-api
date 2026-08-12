# Volcengine Native TTS v3 HTTP Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a native Volcengine TTS v3 HTTP Chunked gateway at `/api/v3/tts/unidirectional` that preserves official request and response records, including word-level timestamps.

**Architecture:** Add a dedicated native relay format and request type so the existing billing, model mapping, quota settlement, diagnostics, and error pipeline remains in use without forcing the native payload through the OpenAI audio DTO. Add a route-specific token adapter that treats `X-Api-Key` as the New API token, then let the Volcengine adaptor replace it with channel credentials and stream the upstream NDJSON response unchanged. Keep `/v1/audio/speech` untouched.

**Tech Stack:** Go 1.22+, Gin, GORM-backed relay pipeline, `common.Marshal`/`common.Unmarshal`, `httptest`, Testify, existing HTTP streaming and diagnostic helpers.

---

### Task 1: Define the native relay contract and route recognition

**Files:**
- Modify: `types/relay_format.go`
- Modify: `relay/constant/relay_mode.go`
- Modify: `router/relay-router.go`
- Test: `relay/constant/relay_mode_test.go` or a new route test beside the relay mode tests

**Step 1: Write the failing tests**

Add tests that assert `/api/v3/tts/unidirectional` maps to a dedicated native TTS relay mode/format and that `/v1/audio/speech` continues mapping to the existing OpenAI audio mode.

**Step 2: Run tests to verify they fail**

Run: `go test ./relay/constant -run NativeTTS -count=1`

Expected: FAIL because the native route and relay format do not exist.

**Step 3: Implement the route contract**

Add a named `RelayFormatVolcengineTTSNative`, a named relay mode for the native HTTP endpoint, and register the POST route. Keep the WebSocket path out of this phase; reserve its mode/path constant for a later implementation only if the existing router requires it.

**Step 4: Run tests to verify they pass**

Run: `go test ./relay/constant -run NativeTTS -count=1`

Expected: PASS.

**Step 5: Commit**

```powershell
git add types/relay_format.go relay/constant/relay_mode.go router/relay-router.go relay/constant/*test.go
git commit -m "feat: add native volcengine tts route"
```

### Task 2: Add native request parsing and model metadata

**Files:**
- Create: `dto/volcengine_tts_native.go`
- Modify: `relay/helper/valid_request.go`
- Modify: `relay/common/relay_info.go`
- Modify: `controller/relay.go`
- Test: `dto/volcengine_tts_native_test.go`, `relay/helper/valid_request_test.go`

**Step 1: Write the failing tests**

Cover valid native JSON with `req_params.text`, `req_params.speaker`, `audio_params.enable_subtitle: true`, and arbitrary provider fields. Cover rejection of malformed JSON, missing text, and missing speaker. Assert that `X-Api-Resource-Id` becomes the model used for permission/channel selection and that explicit `false`, `0`, and empty-but-present native values are not silently rewritten.

**Step 2: Run tests to verify they fail**

Run: `go test ./dto ./relay/helper ./relay/common -run NativeTTS -count=1`

Expected: FAIL because no native request type or parser exists.

**Step 3: Implement the minimal request type**

Create a request type implementing `dto.Request`. Preserve the decoded raw JSON for forwarding, expose the text for token estimation, report native HTTP as non-SSE, and derive the requested model from `X-Api-Resource-Id` with a clear fallback/error when absent. Use `common.UnmarshalBodyReusable`; do not call `encoding/json` marshal/unmarshal directly in business code. Apply the existing request body and billing-safety limits.

**Step 4: Run tests to verify they pass**

Run: `go test ./dto ./relay/helper ./relay/common -run NativeTTS -count=1`

Expected: PASS.

**Step 5: Commit**

```powershell
git add dto/volcengine_tts_native.go dto/volcengine_tts_native_test.go relay/helper/valid_request.go relay/helper/*test.go relay/common/relay_info.go controller/relay.go
git commit -m "feat: parse native volcengine tts requests"
```

### Task 3: Accept `X-Api-Key` as the gateway token only on the native route

**Files:**
- Modify: `middleware/auth.go`
- Modify: `router/relay-router.go`
- Test: `middleware/auth_test.go` or a focused native auth test

**Step 1: Write the failing tests**

Assert that a native request with `X-Api-Key: sk-gateway-token` authenticates like `Authorization: Bearer sk-gateway-token`, while the same header on unrelated routes keeps existing behavior. Assert that the upstream credential is not left in the request context as the caller token.

**Step 2: Run tests to verify they fail**

Run: `go test ./middleware -run NativeTTS -count=1`

Expected: FAIL because the auth middleware only recognizes the current route-specific header conversions.

**Step 3: Implement route-scoped header conversion**

Before normal token lookup, detect only the native Volcengine TTS path and copy a non-empty `X-Api-Key` value into the internal Bearer authorization path. Do not change generic `X-Api-Key` handling on other endpoints. Ensure the outbound header clone is scrubbed or overwritten by the adaptor before upstream transmission.

**Step 4: Run tests to verify they pass**

Run: `go test ./middleware -run NativeTTS -count=1`

Expected: PASS.

**Step 5: Commit**

```powershell
git add middleware/auth.go middleware/*test.go router/relay-router.go
git commit -m "feat: authenticate native tts with x-api-key"
```

### Task 4: Wire native requests into the relay and channel selection pipeline

**Files:**
- Modify: `relay/helper/valid_request.go`
- Modify: `relay/common/relay_info.go`
- Modify: `relay/channel/volcengine/adaptor.go`
- Modify: `relay/channel/adapter.go` if the adaptor interface needs a native method
- Test: `relay/channel/volcengine/native_tts_test.go`, `relay/common/*test.go`

**Step 1: Write the failing tests**

Assert that the native relay format invokes the Volcengine adaptor, uses the `X-Api-Resource-Id` model for channel matching, and rejects a channel configured for a different resource instead of trusting a caller override.

**Step 2: Run tests to verify they fail**

Run: `go test ./relay/channel/volcengine ./relay/common -run NativeTTS -count=1`

Expected: FAIL because the generic relay switch does not know the native format.

**Step 3: Implement the relay wiring**

Add native format handling to request parsing, `GenRelayInfo`, relay mode selection, and the adaptor dispatch. Keep the request conversion chain explicit so native JSON is not converted to an OpenAI audio request. Set the authoritative resource ID from channel settings before creating upstream headers.

**Step 4: Run tests to verify they pass**

Run: `go test ./relay/channel/volcengine ./relay/common -run NativeTTS -count=1`

Expected: PASS.

**Step 5: Commit**

```powershell
git add relay/helper/valid_request.go relay/common/relay_info.go relay/channel/volcengine relay/channel/adapter.go
git commit -m "feat: route native tts through volcengine channels"
```

### Task 5: Implement native HTTP Chunked passthrough and credential replacement

**Files:**
- Create or modify: `relay/channel/volcengine/tts_v3_native_http.go`
- Modify: `relay/channel/volcengine/tts_v3.go`
- Modify: `relay/channel/volcengine/adaptor.go`
- Test: `relay/channel/volcengine/tts_v3_native_http_test.go`

**Step 1: Write the failing tests**

Use an `httptest` upstream that emits multiple newline-delimited records containing base64 audio, `sentence.words`, `sentence.phonemes`, and `usage`. Assert that the downstream body contains the exact records in order, including timestamp fields, and that the upstream receives the channel credential rather than the gateway token. Add a provider-error and malformed-line case.

**Step 2: Run tests to verify they fail**

Run: `go test ./relay/channel/volcengine -run NativeTTSHTTP -count=1`

Expected: FAIL because the native passthrough handler does not exist.

**Step 3: Implement the streaming handler**

Forward the preserved native JSON body to the configured Volcengine v3 endpoint. Generate or forward safe request/connect IDs, set the configured resource ID, and select new-console or legacy authentication from channel settings. Read complete upstream lines with bounded buffering, write each line plus its newline immediately, flush, and retain a copy for usage parsing and diagnostics. Do not decode, concatenate, or rewrite audio data.

**Step 4: Run tests to verify they pass**

Run: `go test ./relay/channel/volcengine -run NativeTTSHTTP -count=1`

Expected: PASS.

**Step 5: Commit**

```powershell
git add relay/channel/volcengine/tts_v3_native_http.go relay/channel/volcengine/tts_v3.go relay/channel/volcengine/adaptor.go relay/channel/volcengine/*native*test.go
git commit -m "feat: proxy native volcengine tts http stream"
```

### Task 6: Integrate usage settlement and diagnostics without changing the native body

**Files:**
- Modify: `relay/audio_handler.go` or add a dedicated native relay handler beside it
- Modify: `service/diagnostic_capture.go` only if the existing streaming capture cannot record native lines
- Test: `relay/channel/volcengine/tts_v3_native_http_test.go`, `service/diagnostic_capture_test.go`

**Step 1: Write the failing tests**

Assert that `usage.text_words` is used for checked settlement while the exact usage record still reaches the client, and that diagnostics redact the gateway token and upstream credential. Assert cancellation closes the upstream request.

**Step 2: Run tests to verify they fail**

Run: `go test ./relay ./relay/channel/volcengine ./service -run NativeTTS -count=1`

Expected: FAIL until the native handler is connected to quota settlement and diagnostic capture.

**Step 3: Implement accounting and capture**

Use the existing checked quota helpers and `relayInfo.QuotaClamp` path. Preserve pre-consume/refund behavior on errors. Record native outbound metadata and response lines through existing diagnostic APIs, applying credential redaction. Ensure downstream cancellation propagates through the request context.

**Step 4: Run tests to verify they pass**

Run: `go test ./relay ./relay/channel/volcengine ./service -run NativeTTS -count=1`

Expected: PASS.

**Step 5: Commit**

```powershell
git add relay/audio_handler.go relay/channel/volcengine service/diagnostic_capture.go service/*test.go
git commit -m "feat: settle and capture native tts streams"
```

### Task 7: Verify regression safety and document client usage

**Files:**
- Modify: `docs/plans/2026-07-27-volcengine-native-tts-v3-design.md` only if behavior changed during implementation
- Create or modify: a concise native TTS usage document under `docs/`
- Test: existing `/v1/audio/speech` and Volcengine v3 test suites

**Step 1: Run focused regression tests**

Run: `go test ./relay/channel/volcengine ./relay ./middleware ./relay/helper -count=1`

Expected: PASS, including existing OpenAI-compatible TTS tests.

**Step 2: Run the full verification suite**

Run: `go test ./... -count=1`

Expected: PASS.

Run: `go build ./...`

Expected: exit code 0.

Run from `web/`: `bun run typecheck` and `bun run build`.

Expected: both commands exit code 0.

**Step 3: Check the final diff and status**

Run: `git diff --check; git status --short --branch`

Expected: no whitespace errors and a clean worktree.

**Step 4: Commit documentation if needed**

```powershell
git add docs
git commit -m "docs: document native volcengine tts endpoint"
```
