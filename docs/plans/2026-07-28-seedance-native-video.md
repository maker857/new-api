# Seedance Native Video API Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `/v1/videos` translate OpenAI width/height into Seedance fields and add a Volcengine-native Seedance task creation endpoint.

**Architecture:** Extend the common task request DTO to retain unrecognized JSON options, then let the Doubao task adaptor merge those options into the native Seedance payload. Known compatibility dimensions are converted to `resolution` and `ratio`. A dedicated native route uses the task pipeline for channel selection, billing, and task persistence while returning the upstream creation payload without converting it to OpenAI video JSON.

**Tech Stack:** Go 1.22, Gin, GORM task persistence, testify, New API task relay/adaptor framework.

---

### Task 1: Preserve compatibility request options and map Seedance dimensions

**Files:**
- Modify: `relay/common/relay_info.go:735-800`
- Modify: `relay/channel/task/doubao/adaptor.go:35-65,280-330`
- Test: `relay/channel/task/doubao/adaptor_test.go`

**Step 1: Write the failing tests**

Add table tests proving that:

```go
TaskSubmitReq{Duration: 15, Metadata: map[string]any{"width": 720, "height": 1280, "seed": 7}}
```

produces a payload containing `duration: 15`, `resolution: "720p"`, `ratio: "9:16"`, and `seed: 7`; and that explicit `resolution`/`ratio` values override inferred dimensions.

**Step 2: Run the focused test to verify it fails**

Run:

```powershell
go test ./relay/channel/task/doubao -run 'Seedance|Duration' -count=1
```

Expected: FAIL because root OpenAI compatibility options are not retained or dimensions are not converted.

**Step 3: Implement the minimal conversion**

- Add an extra-options map to `TaskSubmitReq` populated from unknown top-level JSON members.
- Merge supported extra options into `requestPayload` after metadata parsing.
- Derive `resolution` and `ratio` from recognized `width`/`height` pairs only when native values were not explicitly supplied.
- Keep `duration` and `seed` as native fields.

**Step 4: Run focused tests**

Run the command from Step 2.

Expected: PASS.

**Step 5: Commit**

```powershell
git add relay/common/relay_info.go relay/channel/task/doubao/adaptor.go relay/channel/task/doubao/adaptor_test.go
git commit -m "fix: map openai video dimensions for seedance"
```

### Task 2: Add native Seedance task creation response mode

**Files:**
- Modify: `relay/relay_task.go`
- Modify: `relay/channel/task/doubao/adaptor.go`
- Test: `relay/relay_task_test.go` or a focused new native task response test

**Step 1: Write the failing test**

Create a native request context and assert that a successful Doubao submit writes the upstream JSON body `{"id":"cgt-..."}` instead of the OpenAI queued-video response.

**Step 2: Run the focused test to verify it fails**

Run:

```powershell
go test ./relay -run 'NativeSeedance' -count=1
```

Expected: FAIL because `TaskAdaptor.DoResponse` always emits OpenAI-compatible output.

**Step 3: Implement the minimal response-mode branch**

- Mark the native route in Gin context.
- Preserve the existing task create/persist/billing flow.
- For the native route, return the original successful upstream response bytes and still return the upstream task ID to the task persistence layer.
- Keep `/v1/videos` behavior unchanged.

**Step 4: Run focused tests**

Run the command from Step 2.

Expected: PASS.

**Step 5: Commit**

```powershell
git add relay/relay_task.go relay/channel/task/doubao/adaptor.go relay/relay_task_test.go
git commit -m "feat: return native seedance task responses"
```

### Task 3: Register and validate the official native route

**Files:**
- Modify: `router/video-router.go`
- Modify: `middleware/distributor.go`
- Test: `router/video_router_test.go` or focused route tests
- Test: `middleware/distributor_native_seedance_test.go`

**Step 1: Write failing tests**

Assert that `POST /api/plan/v3/contents/generations/tasks` extracts `model` from its native JSON body and routes to `controller.RelayTask` with normal token authentication, rate limiting, and channel distribution.

**Step 2: Run focused tests to verify failure**

Run:

```powershell
go test ./router ./middleware -run 'NativeSeedance' -count=1
```

Expected: FAIL because the route is not registered.

**Step 3: Implement the route**

- Add a router group for `/api/plan/v3/contents/generations` with `RouteTag`, `SystemPerformanceCheck`, `TokenAuth`, `ModelRequestRateLimit`, and `Distribute`.
- Register `POST /tasks` and set the native response-mode context before invoking `controller.RelayTask`.
- Teach distributor model extraction to read native `model` from this route.

**Step 4: Run focused tests**

Run the command from Step 2.

Expected: PASS.

**Step 5: Commit**

```powershell
git add router/video-router.go middleware/distributor.go router/video_router_test.go middleware/distributor_native_seedance_test.go
git commit -m "feat: add native seedance task endpoint"
```

### Task 4: Verify compatibility and document the native API

**Files:**
- Modify: `docs/volcengine-native-video.md`

**Step 1: Add documentation**

Document both endpoints, OpenAI width/height conversion rules, native field passthrough, and the native request/response example.

**Step 2: Run verification**

```powershell
go test ./... -count=1
go build ./...
git diff --check
```

**Step 3: Commit**

```powershell
git add docs/volcengine-native-video.md
git commit -m "docs: document native seedance video api"
```
