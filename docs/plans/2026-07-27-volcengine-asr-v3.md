# VolcEngine ASR v3 Native Adaptation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add native VolcEngine ASR v3 recording-file submit/query support under the VolcEngine channel type, preserving word-level timestamps.

**Architecture:** Add a dedicated relay mode and native handler parallel to the existing native TTS implementation. Store ASR protocol/resource/auth settings independently from TTS, replace client credentials with channel credentials, and proxy the official `/api/v3/auc/bigmodel/submit` and `/query` JSON contracts without converting responses to Whisper format.

**Tech Stack:** Go 1.22, Gin, GORM-backed channel settings, existing relay/common request helpers, React/TypeScript channel drawer, testify.

---

### Task 1: Add relay modes and route recognition

**Files:**
- Modify: `relay/constant/relay_mode.go`
- Modify: `relay/constant/relay_mode_test.go`
- Modify: `router/relay-router.go`

**Step 1: Write the failing tests**

Add route-to-mode cases for `/api/v3/auc/bigmodel/submit` and `/api/v3/auc/bigmodel/query`.

**Step 2: Run tests to verify failure**

Run: `go test ./relay/constant -run TestPath2RelayMode -count=1`
Expected: FAIL because both paths currently resolve to `RelayModeUnknown`.

**Step 3: Implement minimal route support**

Add a dedicated ASR submit/query relay mode and register both POST routes through the controller using the native VolcEngine ASR helper.

**Step 4: Run tests to verify pass**

Run: `go test ./relay/constant -run TestPath2RelayMode -count=1`
Expected: PASS.

**Step 5: Commit**

```powershell
git add relay/constant/relay_mode.go relay/constant/relay_mode_test.go router/relay-router.go
git commit -m "feat: add native volcengine asr routes"
```

### Task 2: Add ASR channel configuration

**Files:**
- Modify: `dto/channel_settings.go`
- Modify: `dto/channel_settings_test.go`
- Modify: `model/channel.go`
- Modify: `web/src/features/channels/lib/channel-form.ts`
- Modify: `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
- Create: `web/src/features/channels/components/drawers/sections/volcengine-asr-section.tsx`

**Step 1: Write failing validation/serialization tests**

Cover default protocol, required resource ID, supported auth modes, and round-trip JSON serialization for ASR settings.

**Step 2: Run tests to verify failure**

Run: `go test ./dto -run TestVolcASRConfig -count=1` and the focused frontend channel tests.
Expected: FAIL because the ASR config type and serializer do not exist.

**Step 3: Implement minimal configuration**

Add `VolcASRConfig` with native v3 AUC protocol, resource ID, and legacy/new-console auth mode. Validate it only for VolcEngine channels and expose it in the VolcEngine channel drawer, leaving DoubaoVideo unchanged.

**Step 4: Run tests to verify pass**

Run the focused Go and frontend tests.
Expected: PASS.

**Step 5: Commit**

```powershell
git add dto model web/src/features/channels
git commit -m "feat: configure volcengine asr resources"
```

### Task 3: Parse and validate native ASR requests

**Files:**
- Create: `relay/helper/volcengine_asr_native_request.go`
- Create: `relay/helper/volcengine_asr_native_request_test.go`
- Create: `relay/native_asr_handler.go`
- Create: `relay/native_asr_handler_test.go`

**Step 1: Write failing tests**

Test JSON body preservation, request ID extraction/generation, required `show_utterances` handling, missing body rejection, API type rejection, missing config rejection, and resource mismatch rejection.

**Step 2: Run tests to verify failure**

Run: `go test ./relay/helper ./relay -run 'VolcengineASR|NativeVolcengineASR' -count=1`
Expected: FAIL because the request type and helper do not exist.

**Step 3: Implement minimal parsing and validation**

Use the project JSON wrappers, preserve native request fields, and validate only gateway-owned routing/auth fields. Keep timestamp-related request options intact.

**Step 4: Run tests to verify pass**

Run the same focused tests.
Expected: PASS.

**Step 5: Commit**

```powershell
git add relay/helper relay/native_asr_handler.go relay/native_asr_handler_test.go
git commit -m "feat: validate native volcengine asr requests"
```

### Task 4: Proxy ASR submit/query and preserve timestamps

**Files:**
- Create: `relay/channel/volcengine/asr_v3.go`
- Create: `relay/channel/volcengine/asr_v3_native_http.go`
- Create: `relay/channel/volcengine/asr_v3_native_http_test.go`
- Modify: `relay/channel/volcengine/adaptor.go`

**Step 1: Write failing upstream contract tests**

Use an `httptest.Server` to assert endpoint selection, credential replacement, resource/request headers, JSON body forwarding, upstream error propagation, and response bodies containing `utterances.words.start_time/end_time`.

**Step 2: Run tests to verify failure**

Run: `go test ./relay/channel/volcengine -run 'ASR|Asr' -count=1`
Expected: FAIL because the native ASR transport does not exist.

**Step 3: Implement minimal proxy**

Add official submit/query endpoints, auth header construction for configured modes, request cancellation, status handling, and byte-preserving JSON response streaming. Do not emit an OpenAI error after native response bytes have been committed.

**Step 4: Run tests to verify pass**

Run the same focused tests.
Expected: PASS.

**Step 5: Commit**

```powershell
git add relay/channel/volcengine
git commit -m "feat: proxy native volcengine asr v3"
```

### Task 5: Usage accounting, docs, and full verification

**Files:**
- Modify: `relay/native_asr_handler.go`
- Create: `docs/volcengine-native-asr-v3.md`
- Add/update: focused regression tests as needed

**Step 1: Write failing accounting regression test**

Cover safe usage fallback and cancellation/refund behavior without converting unbounded upstream duration values directly to `int`.

**Step 2: Run test to verify failure**

Run the focused relay tests.
Expected: FAIL until accounting is wired.

**Step 3: Implement accounting and documentation**

Use existing quota helpers and logging hooks. Document channel type, ASR resource IDs, required headers, submit/query examples, and the `show_utterances` requirement.

**Step 4: Run all verification**

Run:

```powershell
go test ./... -count=1
go build ./...
cd web
bun run typecheck
bun run build
git diff --check
```

Expected: all commands pass.

**Step 5: Commit**

```powershell
git add relay docs/volcengine-native-asr-v3.md
git commit -m "feat: complete native volcengine asr v3 support"
```

