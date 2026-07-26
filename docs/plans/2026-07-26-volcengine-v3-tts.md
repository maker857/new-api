# VolcEngine v3 Unidirectional TTS Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add VolcEngine Seed-TTS v3 WebSocket unidirectional streaming and HTTP Chunked support to channel type 45 while preserving the existing v1 TTS path.

**Architecture:** Store protocol, resource, auth, and usage settings in the existing channel settings JSON. Convert the OpenAI `/v1/audio/speech` request into VolcEngine v3 session payloads, then dispatch to a WebSocket unidirectional handler or an HTTP Chunked frame handler. Reuse the existing VolcEngine binary `Message` implementation, add strict configuration validation, and keep v1 as the default when no v3 configuration exists.

**Tech Stack:** Go 1.22+, Gin, Gorilla WebSocket, testify, React 19, TypeScript, React Hook Form, Zod, Base UI, Bun, i18next.

---

### Task 1: Add validated VolcEngine TTS channel settings

**Files:**
- Modify: `dto/channel_settings.go:36`
- Modify: `dto/channel_settings_test.go`
- Modify: `model/channel.go:972`
- Modify: `model/channel_settings_test.go`

**Step 1: Write the failing DTO tests**

Add table tests for these contracts:

```go
func TestVolcTTSConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  VolcTTSConfig
		wantErr string
	}{
		{name: "empty keeps v1 compatibility", config: VolcTTSConfig{}},
		{name: "explicit v1", config: VolcTTSConfig{Protocol: VolcTTSProtocolV1WsBinary}},
		{
			name: "websocket v3 requires resource",
			config: VolcTTSConfig{Protocol: VolcTTSProtocolV3WsUni},
			wantErr: "resource_id is required",
		},
		{
			name: "http chunked accepts resource",
			config: VolcTTSConfig{
				Protocol: VolcTTSProtocolV3HTTPChunked,
				ResourceID: "seed-tts-2.0",
				AuthMode: VolcTTSAuthModeNewConsole,
			},
		},
		{
			name: "unknown protocol rejected",
			config: VolcTTSConfig{Protocol: "v3_unknown", ResourceID: "seed-tts-2.0"},
			wantErr: "unsupported volcengine tts protocol",
		},
		{
			name: "unknown auth mode rejected",
			config: VolcTTSConfig{
				Protocol: VolcTTSProtocolV3WsUni,
				ResourceID: "seed-tts-2.0",
				AuthMode: "unknown",
			},
			wantErr: "unsupported volcengine tts auth mode",
		},
	}
	// require/assert table loop
}
```

Add a model-layer regression proving a type 45 channel rejects invalid v3 settings and accepts an empty/v1 setting.

**Step 2: Run tests and verify RED**

Run:

```powershell
go test ./dto ./model -run 'TestVolcTTS|TestChannelValidateSettings.*Volc' -count=1
```

Expected: compile failure because `VolcTTSConfig` and constants do not exist.

**Step 3: Implement the minimal settings model**

Add:

```go
const (
	VolcTTSProtocolV1WsBinary    = "v1_ws_binary"
	VolcTTSProtocolV3WsUni       = "v3_ws_uni"
	VolcTTSProtocolV3HTTPChunked = "v3_http_chunked"

	VolcTTSAuthModeNewConsole = "new_console"
	VolcTTSAuthModeLegacy     = "legacy"
)

type VolcTTSConfig struct {
	Protocol     string `json:"protocol,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	AuthMode     string `json:"auth_mode,omitempty"`
	RequireUsage *bool  `json:"require_usage,omitempty"`
}
```

Add `VolcTTS *VolcTTSConfig` to `ChannelOtherSettings`, plus direct methods for `IsV3`, effective auth mode, usage behavior, and `Validate`. Validation must trim values, reject unsupported enums, and require a non-empty resource ID only for v3.

Call `VolcTTS.Validate()` from `Channel.ValidateSettings()` when type is `constant.ChannelTypeVolcEngine` and the setting exists.

Use `common.Unmarshal*` for all JSON parsing.

**Step 4: Run tests and verify GREEN**

Run the Step 2 command. Expected: PASS.

**Step 5: Commit**

```powershell
git add dto/channel_settings.go dto/channel_settings_test.go model/channel.go model/channel_settings_test.go
git commit -m "feat: validate volcengine tts channel settings"
```

### Task 2: Extend the VolcEngine model list

**Files:**
- Modify: `relay/channel/volcengine/constants.go:3`
- Create: `relay/channel/volcengine/adaptor_test.go`

**Step 1: Write the failing model-list test**

Test the public adaptor contract:

```go
func TestAdaptorGetModelListIncludesSeedTTSResources(t *testing.T) {
	models := (&Adaptor{}).GetModelList()
	assert.Contains(t, models, "seed-tts-1.0-concurr")
	assert.Contains(t, models, "seed-tts-2.0")
	assert.Contains(t, models, "seed-icl-2.0")
}
```

**Step 2: Verify RED**

```powershell
go test ./relay/channel/volcengine -run TestAdaptorGetModelListIncludesSeedTTSResources -count=1
```

Expected: FAIL because the models are absent.

**Step 3: Add the three resource model names**

Append only the approved names to `ModelList`. Do not remove or rename existing protected project or provider models.

**Step 4: Verify GREEN and commit**

```powershell
go test ./relay/channel/volcengine -run TestAdaptorGetModelListIncludesSeedTTSResources -count=1
git add relay/channel/volcengine/constants.go relay/channel/volcengine/adaptor_test.go
git commit -m "feat: add volcengine seed tts models"
```

### Task 3: Make the binary protocol v3-safe

**Files:**
- Modify: `relay/channel/volcengine/protocols.go:209`
- Create: `relay/channel/volcengine/protocols_v3_test.go`

**Step 1: Write failing round-trip protocol tests**

Cover exact observable frame contracts for:

- `StartConnection`
- `StartSession`
- `FinishSession`
- `ConnectionStarted`
- `ConnectionFinished`
- `AudioOnlyServer` with sequence and payload
- `MsgTypeError`

Each case must marshal a `Message`, parse it with `NewMessageFromBytes`, and assert all public fields. Include a regression proving `ConnectionFinished` writes and reads connection/session fields symmetrically.

**Step 2: Verify RED**

```powershell
go test ./relay/channel/volcengine -run 'TestV3Message|TestConnectionFinished' -count=1
```

Expected: at least the `ConnectionFinished` symmetry case fails.

**Step 3: Apply the minimal protocol corrections**

Port only the required frame-order corrections from upstream PR `#4710`:

- keep event metadata ordering symmetric between `writers()` and `readers()`;
- include `ConnectionFinished` in the correct session/connect ID branches;
- add a small `ParseFrame([]byte)` shim only if it removes duplicate parsing between WS and HTTP handlers;
- add an event-message constructor only if used by both transport implementations.

Do not add bidirectional or SSE-specific behavior.

**Step 4: Verify GREEN and commit**

```powershell
go test ./relay/channel/volcengine -run 'TestV3Message|TestConnectionFinished' -count=1
git add relay/channel/volcengine/protocols.go relay/channel/volcengine/protocols_v3_test.go
git commit -m "fix: align volcengine v3 protocol frames"
```

### Task 4: Add shared v3 request, auth, endpoint, and usage logic

**Files:**
- Create: `relay/channel/volcengine/tts_v3.go`
- Create: `relay/channel/volcengine/tts_v3_test.go`
- Modify: `relay/channel/volcengine/adaptor.go:49`

**Step 1: Write failing shared behavior tests**

Test:

- new-console API key produces `X-Api-Key`;
- legacy `appid|access_token` produces `X-Api-App-Id` and `X-Api-Access-Key`;
- legacy mode rejects a single-segment key;
- every v3 request sends `X-Api-Resource-Id` and `X-Api-Connect-Id`;
- `require_usage` defaults true and can be explicitly disabled;
- OpenAI input, voice, response format, sample rate, bitrate, and speed map to the v3 session payload;
- `usage.text_words` maps to `dto.Usage` without a bare float-to-int cast;
- endpoint resolution returns only the approved WS-unidirectional and HTTP-Chunked URLs.

Use exact header and JSON assertions. JSON encoding/decoding must go through `common.Marshal` and `common.Unmarshal`.

**Step 2: Verify RED**

```powershell
go test ./relay/channel/volcengine -run 'TestV3Auth|TestV3StartSession|TestV3Usage|TestV3Endpoint' -count=1
```

Expected: compile failure because shared v3 functions do not exist.

**Step 3: Implement shared v3 domain logic**

Create stable domain functions such as:

```go
func buildV3AuthHeaders(apiKey string, cfg dto.VolcTTSConfig, connectID string) (http.Header, error)
func buildV3StartSessionPayload(req VolcengineTTSRequest, encoding string) ([]byte, error)
func parseV3Usage(payload []byte, fallback int) (*dto.Usage, error)
func getV3TTSEndpoint(protocol string) (string, error)
```

Keep auth strict:

- `new_console`: non-empty single API key, sent verbatim as `X-Api-Key`;
- `legacy`: exactly `appid|access_token`, both segments non-empty;
- never place secrets in returned error messages or logs.

The v3 payload must include complete text once, the selected speaker, and mapped audio options. Do not accept request metadata overrides for protocol, auth mode, or resource ID.

Use bounded/saturating quota conversion helpers if an upstream numeric usage value can exceed an `int32` billing field.

**Step 4: Verify GREEN and commit**

```powershell
go test ./relay/channel/volcengine -run 'TestV3Auth|TestV3StartSession|TestV3Usage|TestV3Endpoint' -count=1
git add relay/channel/volcengine/tts_v3.go relay/channel/volcengine/tts_v3_test.go relay/channel/volcengine/adaptor.go
git commit -m "feat: add volcengine v3 tts request mapping"
```

### Task 5: Implement WebSocket unidirectional streaming

**Files:**
- Create: `relay/channel/volcengine/tts_v3_ws.go`
- Modify: `relay/channel/volcengine/tts_v3_test.go`
- Modify: `relay/channel/volcengine/adaptor.go:276`

**Step 1: Write a failing end-to-end WebSocket test**

Use `httptest.Server` plus a Gorilla WebSocket upgrader as the upstream fixture. The fixture must:

1. inspect v3 auth headers;
2. receive `StartConnection`;
3. return `ConnectionStarted`;
4. receive `StartSession` with complete text;
5. return `SessionStarted`;
6. return two audio events;
7. return `SessionFinished` with `usage.text_words`;
8. observe the client's finish sequence.

Assert the downstream Gin recorder contains the concatenated audio bytes and the returned usage contains the upstream word count.

Add a deterministic cancellation test using a canceled request context and synchronization channels, not fixed sleeps.

**Step 2: Verify RED**

```powershell
go test ./relay/channel/volcengine -run 'TestV3WSUnidirectional|TestV3WSCancellation' -count=1
```

Expected: compile failure because the handler is absent.

**Step 3: Implement the handler**

Implement the official one-shot state machine only. Requirements:

- dial with `c.Request.Context()`;
- derive proxy/TLS dial behavior from the channel relay HTTP transport where practical;
- set a bounded handshake timeout;
- set a sliding per-frame read deadline;
- run a cancellation watcher that closes the connection when the downstream context ends and exits cleanly when the handler completes;
- record outbound request/response/frame diagnostics through existing service helpers;
- stream only audio payloads to `c.Writer` and flush after each chunk;
- treat upstream error events as provider errors;
- return normally for downstream cancellation instead of misclassifying it as an upstream outage.

Do not implement bidirectional text streaming.

**Step 4: Wire adaptor dispatch**

For `v3_ws_uni`, `GetRequestURL` returns the official endpoint. `DoRequest` skips the generic HTTP relay. `DoResponse` obtains the converted request from Gin context and calls the WS handler. Empty/v1 settings continue to use the existing handler.

**Step 5: Verify GREEN and commit**

```powershell
go test ./relay/channel/volcengine -run 'TestV3WSUnidirectional|TestV3WSCancellation' -count=1
git add relay/channel/volcengine/tts_v3_ws.go relay/channel/volcengine/tts_v3_test.go relay/channel/volcengine/adaptor.go
git commit -m "feat: stream volcengine v3 tts over websocket"
```

### Task 6: Implement HTTP Chunked streaming

**Files:**
- Create: `relay/channel/volcengine/tts_v3_http.go`
- Modify: `relay/channel/volcengine/tts_v3_test.go`
- Modify: `relay/channel/volcengine/adaptor.go:276`

**Step 1: Write failing HTTP Chunked tests**

Use `httptest.Server` to return concatenated official v3 frames in deliberately split network writes. Assert:

- frame boundaries survive arbitrary `Read` boundaries;
- audio frames are concatenated into the downstream response;
- `SessionFinished` usage is returned;
- provider error frames become a `NewAPIError`;
- HTTP non-200 bodies are bounded and included without leaking credentials;
- `X-Tt-Logid` is exposed as `X-Volc-Logid`.

**Step 2: Verify RED**

```powershell
go test ./relay/channel/volcengine -run 'TestV3HTTPChunked|TestV3ChunkedFrameReader' -count=1
```

Expected: compile failure because the frame reader and handler are absent.

**Step 3: Implement the streaming frame reader and handler**

Port only the HTTP Chunked splitter needed from PR `#4710`. It must use `io.ReadFull` and explicit size bounds rather than assuming each `Read` returns one frame.

Build a streaming client from the configured relay/proxy transport:

- clone the transport so shared clients are not mutated;
- preserve configured HTTP/SOCKS proxy behavior;
- set dial, TLS handshake, and response header timeouts;
- set `Client.Timeout` to zero for the streaming body;
- close idle connections after the request finishes.

Create the upstream request with `c.Request.Context()`. Use `service.PrepareDiagnosticOutboundRequest` and response metadata recording.

**Step 4: Wire adaptor dispatch**

For `v3_http_chunked`, skip the generic relay request and call the HTTP handler from `DoResponse`. Keep the endpoint fixed to the official OpenSpeech host when the standard type-45 base URL is used.

**Step 5: Verify GREEN and commit**

```powershell
go test ./relay/channel/volcengine -run 'TestV3HTTPChunked|TestV3ChunkedFrameReader' -count=1
git add relay/channel/volcengine/tts_v3_http.go relay/channel/volcengine/tts_v3_test.go relay/channel/volcengine/adaptor.go
git commit -m "feat: stream volcengine v3 tts over http chunked"
```

### Task 7: Add audit information and v1 regression coverage

**Files:**
- Modify: `service/log_info_generate.go:259`
- Modify: `service/text_quota_test.go` or the nearest existing audio log test
- Modify: `relay/channel/volcengine/adaptor_test.go`

**Step 1: Write failing audit and regression tests**

Assert that v3 audio logs include:

```json
{
  "volc_tts_protocol": "v3_http_chunked",
  "volc_tts_resource_id": "seed-tts-2.0"
}
```

Also assert:

- no v3 fields are added for an unrelated provider;
- empty VolcEngine settings resolve to v1 URL and existing auth/request behavior;
- the current v1 WebSocket response path remains selected.

**Step 2: Verify RED**

```powershell
go test ./service ./relay/channel/volcengine -run 'TestGenerateAudioOtherInfo.*Volc|TestAdaptor.*V1Fallback' -count=1
```

**Step 3: Add audit fields at the existing audio log boundary**

Use the resolved channel setting already available on `relayInfo`. Do not create a second source of truth or expose secrets.

**Step 4: Verify GREEN and commit**

```powershell
go test ./service ./relay/channel/volcengine -run 'TestGenerateAudioOtherInfo.*Volc|TestAdaptor.*V1Fallback' -count=1
git add service/log_info_generate.go service/text_quota_test.go relay/channel/volcengine/adaptor_test.go
git commit -m "feat: audit volcengine tts protocol usage"
```

### Task 8: Add the type-45 management UI

**Required skill before edits:** Read and follow `.agents/skills/i18n-translate/SKILL.md` because this task adds frontend translations.

**Files:**
- Create: `web/src/features/channels/lib/volcengine-tts.ts`
- Create: `web/src/features/channels/lib/__tests__/volcengine-tts.test.ts`
- Create: `web/src/features/channels/components/drawers/sections/volcengine-tts-section.tsx`
- Modify: `web/src/features/channels/components/drawers/sections/index.ts`
- Modify: `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx:2693`
- Modify: `web/src/features/channels/lib/channel-form.ts:135`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/zh.json`
- Modify other locale files through the project i18n workflow

**Step 1: Write failing pure-form tests**

Using `node:test`, cover:

- parsing `settings.volc_tts` into form defaults;
- serializing type 45 fields while preserving unrelated settings keys;
- removing `volc_tts` when changing away from type 45;
- rejecting v3 protocols without a resource ID;
- rejecting legacy auth with a non-`appid|access_token` key;
- accepting empty settings for v1 compatibility.

Run:

```powershell
Set-Location web
bun test src/features/channels/lib/__tests__/volcengine-tts.test.ts
```

Expected: FAIL because the domain helper and form fields do not exist.

**Step 2: Implement the pure settings helper and form schema**

Export typed protocol/auth constants and small parse/serialize functions from `volcengine-tts.ts`. Add these fields to `ChannelFormValues`:

```ts
volc_tts_protocol: z.enum([
  'v1_ws_binary',
  'v3_ws_uni',
  'v3_http_chunked',
]).optional(),
volc_tts_resource_id: z.string().optional(),
volc_tts_auth_mode: z.enum(['new_console', 'legacy']).optional(),
volc_tts_require_usage: z.boolean().optional(),
```

Add `superRefine` validation only for type 45. Update defaults, API-to-form parsing, and `buildSettingsJSON` serialization. Preserve unknown settings keys.

**Step 3: Verify helper tests GREEN**

Run the Step 1 command. Expected: PASS.

**Step 4: Build the form section**

Use existing Base UI `Select`, `Input`, and `Switch` controls. The section is visible only for type 45 and contains:

- protocol select;
- resource ID input with known values available as suggestions or documented examples;
- auth mode select;
- require-usage switch;
- contextual key-format description.

Do not put this section inside a decorative nested card. Use `useTranslation()` in the component and stable semantic i18n keys.

**Step 5: Sync translations**

Add English and Chinese source translations, then follow the `i18n-translate` skill for `zh-TW`, `fr`, `ru`, `ja`, and `vi`.

Run:

```powershell
Set-Location web
bun run i18n:sync
```

Review the diff to ensure the sync tool did not overwrite protected or unrelated content.

**Step 6: Run frontend checks and commit**

```powershell
Set-Location web
bun test src/features/channels/lib/__tests__/volcengine-tts.test.ts
bun run typecheck
bun run lint
bun run build
Set-Location ..
git add web/src/features/channels
git add web/src/i18n/locales
git commit -m "feat: configure volcengine v3 tts channels"
```

Expected: all commands succeed without TypeScript or lint errors.

### Task 9: Run full verification and local smoke checks

**Files:**
- Verify all modified files
- Do not alter the existing uncommitted Seedance duration changes except to ensure they still pass

**Step 1: Format and inspect**

```powershell
gofmt -w dto/channel_settings.go dto/channel_settings_test.go model/channel.go model/channel_settings_test.go relay/channel/volcengine/*.go service/log_info_generate.go
git diff --check
git status --short
```

Confirm only intended files plus the pre-existing Seedance duration files are modified.

**Step 2: Run focused backend tests**

```powershell
go test ./dto ./model ./relay/channel/volcengine ./service -count=1
```

Expected: PASS.

**Step 3: Run full backend verification**

```powershell
go test ./... -count=1
go build ./...
```

Expected: PASS.

**Step 4: Re-run frontend verification**

```powershell
Set-Location web
bun run typecheck
bun run lint
bun run build
Set-Location ..
```

Expected: PASS.

**Step 5: Rebuild the local backend**

```powershell
docker compose -f docker-compose.dev.yml up -d --build new-api
docker compose -f docker-compose.dev.yml ps
```

Expected: `new-api-dev` and dependencies are running; PostgreSQL is healthy.

**Step 6: Perform no-secret smoke validation**

Without transmitting credentials, verify:

- a v3 type-45 channel cannot be saved without `resource_id`;
- legacy auth rejects an invalid key format;
- an existing v1 channel still resolves to `ws_binary`;
- the service responds normally on `http://localhost:3000`.

Live upstream audio generation requires the user's valid VolcEngine credentials and matching speaker/resource family. Report it as unverified if those credentials are not available.

**Step 7: Final commit if formatting or integration changed files**

```powershell
git add <only intended integration files>
git commit -m "test: verify volcengine v3 tts integration"
```

Skip this commit when there are no additional changes.
