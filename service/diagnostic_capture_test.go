package service

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticJSONStreamValid(t *testing.T) {
	cases := []struct {
		name  string
		input string
		valid bool
	}{
		{name: "nested", input: `{"items":[{"name":"a\\u4e2d","value":-1.25e+3}],"ok":true}`, valid: true},
		{name: "scalar", input: `null`, valid: true},
		{name: "trailing comma", input: `{"items":[1,]}`, valid: false},
		{name: "unfinished string", input: `{"name":"unterminated}`, valid: false},
		{name: "invalid number", input: `01`, valid: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := diagnosticJSONStreamValid(bufio.NewReader(strings.NewReader(tt.input)))
			require.NoError(t, err)
			require.Equal(t, tt.valid, valid)
		})
	}
}

func TestDiagnosticCaptureChannelEnabledRequiresResolvedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.False(t, diagnosticCaptureChannelEnabled(c, 0))
}

func TestNextDiagnosticCaptureReconciliationRespectsConfiguredSchedule(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	tests := []struct {
		name string
		now  time.Time
		cfg  DiagnosticCaptureConfig
		want time.Time
	}{
		{
			name: "daily moves to tomorrow after scheduled time",
			now:  time.Date(2026, time.August, 8, 3, 1, 0, 0, location),
			cfg:  DiagnosticCaptureConfig{ReconciliationMode: diagnosticCaptureScheduleDaily, ReconciliationHour: 3},
			want: time.Date(2026, time.August, 9, 3, 0, 0, 0, location),
		},
		{
			name: "weekly uses configured weekday",
			now:  time.Date(2026, time.August, 8, 2, 0, 0, 0, location),
			cfg:  DiagnosticCaptureConfig{ReconciliationMode: diagnosticCaptureScheduleWeekly, ReconciliationHour: 3, ReconciliationWeekday: int(time.Monday)},
			want: time.Date(2026, time.August, 10, 3, 0, 0, 0, location),
		},
		{
			name: "monthly day 31 uses last day in short month",
			now:  time.Date(2026, time.February, 1, 2, 0, 0, 0, location),
			cfg:  DiagnosticCaptureConfig{ReconciliationMode: diagnosticCaptureScheduleMonthly, ReconciliationHour: 3, ReconciliationMonthday: 31},
			want: time.Date(2026, time.February, 28, 3, 0, 0, 0, location),
		},
		{
			name: "monthly day 31 advances to the next calendar month",
			now:  time.Date(2026, time.February, 28, 4, 0, 0, 0, location),
			cfg:  DiagnosticCaptureConfig{ReconciliationMode: diagnosticCaptureScheduleMonthly, ReconciliationHour: 3, ReconciliationMonthday: 31},
			want: time.Date(2026, time.March, 31, 3, 0, 0, 0, location),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, nextDiagnosticCaptureReconciliation(tt.now, tt.cfg))
		})
	}
}

func TestDiagnosticCaptureChannelDefaultIsDisabled(t *testing.T) {
	require.False(t, (model.ChannelInfo{}).IsDiagnosticCaptureEnabled())
}

func TestValidateDiagnosticCaptureRelativeDirectory(t *testing.T) {
	absolutePath, err := filepath.Abs("captures")
	require.NoError(t, err)

	cases := []struct {
		name  string
		path  string
		valid bool
	}{
		{name: "directory", path: "captures", valid: true},
		{name: "nested directory", path: "archive/captures", valid: true},
		{name: "absolute path", path: absolutePath, valid: false},
		{name: "parent directory", path: "../captures", valid: false},
		{name: "storage root", path: ".", valid: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDiagnosticCaptureRelativeDirectory(tt.path)
			if tt.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}

func TestDiagnosticCaptureAuxiliaryDirectoriesFollowCaptureDirectory(t *testing.T) {
	for _, captureDir := range []string{"captures", "archive/captures"} {
		t.Run(captureDir, func(t *testing.T) {
			parent := filepath.Dir(captureDir)
			require.Equal(t, filepath.Join(parent, "diagnostic-capture-temp"), diagnosticCaptureTempDir(captureDir))
			require.Equal(t, filepath.Join(parent, "diagnostic-capture-failures"), diagnosticCaptureFailureDir(captureDir))
		})
	}
}

func TestDiagnosticCaptureHeadersRedactCredentials(t *testing.T) {
	headers := map[string][]string{
		"Authorization": {"Bearer diagnostic-token"},
		"X-Api-Key":     {"channel-secret-key"},
		"User-Agent":    {"diagnostic-test"},
		"X-Multi":       {"first", "second"},
	}

	captured := redactHeaders(headers)
	require.Equal(t, "Bearer...-token", captured["Authorization"])
	require.Equal(t, "channe...et-key", captured["X-Api-Key"])
	require.Equal(t, "diagnostic-test", captured["User-Agent"])
	require.Equal(t, "first, second", captured["X-Multi"])
	require.Equal(t, "shor...-key", partiallyRedactDiagnosticHeader("short-key"))
}

func TestCleanupDiagnosticCaptureCandidatesRespectsRetention(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old")
	recentPath := filepath.Join(dir, "recent")
	require.NoError(t, os.MkdirAll(oldPath, 0o755))
	require.NoError(t, os.MkdirAll(recentPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "request-log.json"), []byte("1234"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(recentPath, "request-log.json"), []byte("5678"), 0o600))

	now := time.Now()
	remaining, deletedCount, freedBytes, cutoff := cleanupDiagnosticCaptureCandidates(
		8,
		[]diagnosticCaptureCandidate{
			{dir: oldPath, size: 4, capturedAt: now.Add(-2 * time.Hour), complete: true},
			{dir: recentPath, size: 4, capturedAt: now.Add(-30 * time.Minute), complete: true},
		},
		0,
		now.Add(-time.Hour),
		time.Time{},
		0,
	)

	require.EqualValues(t, 4, remaining)
	require.EqualValues(t, 1, deletedCount)
	require.EqualValues(t, 4, freedBytes)
	require.EqualValues(t, now.Add(-2*time.Hour).Unix(), cutoff)
	_, err := os.Stat(oldPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(recentPath)
	require.NoError(t, err)
}

func TestCleanupDiagnosticCaptureCandidatesAllowsExpiredIncompleteRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incomplete")
	require.NoError(t, os.MkdirAll(path, 0o755))

	now := time.Now()
	remaining, deletedCount, freedBytes, cutoff := cleanupDiagnosticCaptureCandidates(
		4,
		[]diagnosticCaptureCandidate{{
			dir:        path,
			size:       4,
			capturedAt: now.Add(-25 * time.Hour),
		}},
		0,
		now.Add(-time.Hour),
		now.Add(-24*time.Hour),
		0,
	)

	require.EqualValues(t, 0, remaining)
	require.EqualValues(t, 1, deletedCount)
	require.EqualValues(t, 4, freedBytes)
	require.EqualValues(t, now.Add(-25*time.Hour).Unix(), cutoff)
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCleanupDiagnosticCaptureCandidatesKeepsActiveIncompleteRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active")
	require.NoError(t, os.MkdirAll(path, 0o755))

	markDiagnosticCaptureActive("active")
	t.Cleanup(func() { markDiagnosticCaptureInactive("active") })
	remaining, deletedCount, freedBytes, _ := cleanupDiagnosticCaptureCandidates(
		4,
		[]diagnosticCaptureCandidate{{
			dir:        path,
			traceID:    "active",
			size:       4,
			capturedAt: time.Now().Add(-48 * time.Hour),
		}},
		0,
		time.Now().Add(-time.Hour),
		time.Now().Add(-24*time.Hour),
		0,
	)

	require.EqualValues(t, 4, remaining)
	require.Zero(t, deletedCount)
	require.Zero(t, freedBytes)
	_, err := os.Stat(path)
	require.NoError(t, err)
}

func TestCleanupDiagnosticCaptureStorageAlsoRemovesExpiredFailureRecords(t *testing.T) {
	root := t.TempDir()
	failureDir := filepath.Join(root, "diagnostic-capture-failures")
	failurePath := filepath.Join(failureDir, "channel", "2026-07-23", "failed")
	require.NoError(t, os.MkdirAll(failurePath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(failurePath, "capture-failure.json"), []byte(`{"last_error":"disk error"}`), 0o600))
	oldCapturedAt := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.WriteFile(filepath.Join(failurePath, ".capture-created-at"), []byte(strconv.FormatInt(oldCapturedAt.UnixNano(), 10)), 0o600))

	totalBytes, err := diagnosticCaptureDirectorySize(failureDir)
	require.NoError(t, err)
	remaining, deletedCount, freedBytes, _ := cleanupDiagnosticCaptureStorageByDate(
		totalBytes,
		DiagnosticCaptureConfig{CaptureDir: filepath.Join(root, "captures"), FailureDir: failureDir},
		"",
		0,
		time.Now(),
		time.Now().Add(-time.Hour),
	)

	require.Zero(t, remaining)
	require.EqualValues(t, 1, deletedCount)
	require.EqualValues(t, totalBytes, freedBytes)
	require.NoDirExists(t, failurePath)
	require.NoDirExists(t, filepath.Join(failureDir, "channel"))
	require.DirExists(t, failureDir)
}

func TestCleanupDiagnosticCaptureStorageKeepsRetryableFailureRecord(t *testing.T) {
	root := t.TempDir()
	failureDir := filepath.Join(root, "diagnostic-capture-failures")
	failurePath := filepath.Join(failureDir, "channel", "2026-07-23", "retrying")
	require.NoError(t, os.MkdirAll(failurePath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(failurePath, "fragment.part"), []byte("body"), 0o600))
	now := time.Now()
	require.NoError(t, writeDiagnosticCaptureFailureRecord(filepath.Join(failurePath, "capture-failure.json"), diagnosticCaptureFailureRecord{
		TraceID:       "retrying",
		Channel:       "channel",
		StartedAt:     now.Add(-time.Minute).UnixNano(),
		FirstFailedAt: now.Add(-time.Minute).UnixNano(),
		Retryable:     true,
	}))
	require.NoError(t, os.WriteFile(filepath.Join(failurePath, ".capture-created-at"), []byte(strconv.FormatInt(now.Add(-time.Hour).UnixNano(), 10)), 0o600))

	totalBytes, err := diagnosticCaptureDirectorySize(failureDir)
	require.NoError(t, err)
	remaining, deletedCount, freedBytes, _ := cleanupDiagnosticCaptureStorageByDate(
		totalBytes,
		DiagnosticCaptureConfig{CaptureDir: filepath.Join(root, "captures"), FailureDir: failureDir},
		"",
		0,
		now,
		now,
	)

	require.Equal(t, totalBytes, remaining)
	require.Zero(t, deletedCount)
	require.Zero(t, freedBytes)
	require.DirExists(t, failurePath)
}

func TestCleanupDiagnosticCaptureStorageDeletesOldestAcrossChannels(t *testing.T) {
	root := t.TempDir()
	captureDir := filepath.Join(root, "captures")
	now := time.Now()
	newer := filepath.Join(captureDir, "a-channel", "2026-07-23", "newer")
	older := filepath.Join(captureDir, "z-channel", "2026-07-23", "older")
	for _, candidate := range []struct {
		dir        string
		capturedAt time.Time
	}{
		{dir: newer, capturedAt: now.Add(-2 * time.Hour)},
		{dir: older, capturedAt: now.Add(-3 * time.Hour)},
	} {
		require.NoError(t, os.MkdirAll(candidate.dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(candidate.dir, "request-log.json"), []byte("1234"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(candidate.dir, ".capture-created-at"), []byte(strconv.FormatInt(candidate.capturedAt.UnixNano(), 10)), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(candidate.dir, ".capture-complete"), nil, 0o600))
	}

	totalBytes, err := diagnosticCaptureDirectorySize(captureDir)
	require.NoError(t, err)
	remaining, deletedCount, _, _ := cleanupDiagnosticCaptureStorageByDate(
		totalBytes,
		DiagnosticCaptureConfig{CaptureDir: captureDir},
		"",
		totalBytes-1,
		now,
		time.Time{},
	)

	require.Less(t, remaining, totalBytes)
	require.EqualValues(t, 1, deletedCount)
	require.NoDirExists(t, older)
	require.DirExists(t, newer)
}

func TestScanDiagnosticCaptureStorageOnlyReturnsMarkedCaptureDirectories(t *testing.T) {
	dir := t.TempDir()
	marked := filepath.Join(dir, "channel", "2026-07-23", "marked")
	unmarked := filepath.Join(dir, "other", "2026-07-23", "unmarked")
	require.NoError(t, os.MkdirAll(marked, 0o755))
	require.NoError(t, os.MkdirAll(unmarked, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(marked, "request-log.json"), []byte("1234"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(marked, ".capture-created-at"), []byte("1000000000"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(marked, ".capture-complete"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(unmarked, "request-log.json"), []byte("5678"), 0o600))

	totalBytes, candidates, err := scanDiagnosticCaptureStorage(dir, "")
	require.NoError(t, err)
	require.Positive(t, totalBytes)
	require.Len(t, candidates, 1)
	require.Equal(t, marked, candidates[0].dir)
	require.True(t, candidates[0].complete)
}

func TestScanDiagnosticCaptureTotalStorageIncludesTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	captureDir := filepath.Join(root, "captures")
	tempDir := diagnosticCaptureTempDir(captureDir)
	capturePath := filepath.Join(captureDir, "channel", "2026-07-23", "trace")
	require.NoError(t, os.MkdirAll(capturePath, 0o755))
	require.NoError(t, os.MkdirAll(tempDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(capturePath, "request-log.json"), []byte("1234"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(capturePath, ".capture-created-at"), []byte("1000000000"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "active.part"), []byte("5678"), 0o600))

	totalBytes, candidates, err := scanDiagnosticCaptureTotalStorage(DiagnosticCaptureConfig{
		CaptureDir: captureDir,
		TempDir:    tempDir,
	}, "")

	require.NoError(t, err)
	require.EqualValues(t, 4+len("1000000000")+4, totalBytes)
	require.Len(t, candidates, 1)
}

func TestDiagnosticCaptureStorageStatusUsesCachedUsage(t *testing.T) {
	root := t.TempDir()
	captureDir := filepath.Join(root, "captures")
	tempDir := diagnosticCaptureTempDir(captureDir)
	capturePath := filepath.Join(captureDir, "channel", "2026-07-23", "trace")
	require.NoError(t, os.MkdirAll(capturePath, 0o755))
	require.NoError(t, os.MkdirAll(tempDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(capturePath, "request-log.json"), []byte("1234"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(capturePath, ".capture-created-at"), []byte("1000000000"), 0o600))

	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = map[string]string{
		DiagnosticCaptureAutoCleanupEnabledKey: "true",
		DiagnosticCaptureDirKey:                captureDir,
		DiagnosticCaptureMaxStorageBytesKey:    "1000",
		DiagnosticCaptureLastCleanupStatusKey:  "retention_limited",
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
	})

	diagnosticCaptureStorageState.Lock()
	originalCaptureDir := diagnosticCaptureStorageState.captureDir
	originalTempDir := diagnosticCaptureStorageState.tempDir
	originalFailureDir := diagnosticCaptureStorageState.failureDir
	originalTotalBytes := diagnosticCaptureStorageState.totalBytes
	originalInitialized := diagnosticCaptureStorageState.initialized
	originalLastAttempt := diagnosticCaptureStorageState.lastAttempt
	originalLastReconciled := diagnosticCaptureStorageState.lastReconciled
	diagnosticCaptureStorageState.captureDir = captureDir
	diagnosticCaptureStorageState.tempDir = tempDir
	diagnosticCaptureStorageState.failureDir = diagnosticCaptureFailureDir(captureDir)
	diagnosticCaptureStorageState.totalBytes = 777
	diagnosticCaptureStorageState.initialized = true
	diagnosticCaptureStorageState.lastAttempt = time.Time{}
	diagnosticCaptureStorageState.lastReconciled = time.Now()
	diagnosticCaptureStorageState.Unlock()
	t.Cleanup(func() {
		diagnosticCaptureStorageState.Lock()
		diagnosticCaptureStorageState.captureDir = originalCaptureDir
		diagnosticCaptureStorageState.tempDir = originalTempDir
		diagnosticCaptureStorageState.failureDir = originalFailureDir
		diagnosticCaptureStorageState.totalBytes = originalTotalBytes
		diagnosticCaptureStorageState.initialized = originalInitialized
		diagnosticCaptureStorageState.lastAttempt = originalLastAttempt
		diagnosticCaptureStorageState.lastReconciled = originalLastReconciled
		diagnosticCaptureStorageState.Unlock()
	})

	status, err := GetDiagnosticCaptureStorageStatus()
	require.NoError(t, err)
	require.EqualValues(t, 777, status.CurrentBytes)
	require.Empty(t, status.LastCleanupStatus)
	require.FileExists(t, filepath.Join(capturePath, "request-log.json"))
}

func TestMarkDiagnosticCaptureCompleteCreatesMarkerBesideCapture(t *testing.T) {
	dir := t.TempDir()
	started := time.Date(2026, 7, 23, 10, 0, 0, 0, time.Local)
	flow := &DiagnosticFlow{TraceID: "trace", Channel: "channel", Started: started}
	captureDir := filepath.Join(dir, "channel", "2026-07-23", "trace")
	require.NoError(t, os.MkdirAll(captureDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(captureDir, "request-log.json"), []byte("{}"), 0o600))

	markDiagnosticCaptureComplete(DiagnosticCaptureConfig{CaptureDir: dir}, flow)

	info, err := os.Stat(filepath.Join(captureDir, ".capture-complete"))
	require.NoError(t, err)
	require.False(t, info.IsDir())
}

func TestWriteDiagnosticCaptureSessionStoresBodyInSingleJSONFile(t *testing.T) {
	captureDir := t.TempDir()
	tempDir := t.TempDir()
	tempFile, err := os.CreateTemp(tempDir, "request-*.part")
	require.NoError(t, err)
	_, err = tempFile.WriteString(`{"message":"complete"}`)
	require.NoError(t, err)
	require.NoError(t, tempFile.Close())

	flow := &DiagnosticFlow{
		TraceID: "trace",
		Channel: "channel",
		Started: time.Date(2026, 7, 23, 10, 0, 0, 0, time.Local),
	}
	state := &diagnosticCapturePartState{
		partID:       "inbound-request",
		role:         "inbound",
		part:         "request",
		meta:         map[string]any{"method": "POST", "path": "/v1/chat/completions"},
		tempPath:     tempFile.Name(),
		originalSize: int64(len(`{"message":"complete"}`)),
		savedSize:    int64(len(`{"message":"complete"}`)),
		complete:     true,
		jsonBody:     true,
	}

	require.NoError(t, writeDiagnosticCaptureSession(DiagnosticCaptureConfig{
		Enabled:    true,
		Mode:       "full",
		CaptureDir: captureDir,
	}, flow, []*diagnosticCapturePartState{state}))

	path := filepath.Join(captureDir, "channel", "2026-07-23", "trace", "request-log.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var combined diagnosticCombinedCPAJSON
	require.NoError(t, common.Unmarshal(data, &combined))
	require.Equal(t, "trace", combined.NewAPIRequestID)
	require.NotNil(t, combined.Request)
	require.Equal(t, "json", combined.Request.Body.Encoding)
	require.Empty(t, combined.Request.Body.Text)
	require.Equal(t, map[string]any{"message": "complete"}, combined.Request.Body.JSON)
	require.Contains(t, string(data), "\n  \"request\": {")
	require.Contains(t, string(data), "\n      \"original_size\": 22")
	require.Contains(t, string(data), "\n      \"truncated\": false")
	require.Contains(t, string(data), "\n        \"message\": \"complete\"")
	require.NotContains(t, string(data), `"text"`)
	require.NotContains(t, string(data), `"body_original_size"`)
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 2)
}

func TestDiagnosticCaptureOutboundTransportFailureIsRecorded(t *testing.T) {
	captureDir := t.TempDir()
	flow := &DiagnosticFlow{
		TraceID: "transport-failure-trace",
		Channel: "channel",
		Started: time.Date(2026, 7, 24, 10, 0, 0, 0, time.Local),
	}
	flow.session = newDiagnosticCaptureSession(DiagnosticCaptureConfig{
		Enabled:    true,
		Mode:       "full",
		CaptureDir: captureDir,
		TempDir:    filepath.Join(t.TempDir(), "temp"),
	}, flow)
	require.NotNil(t, flow.session)

	RecordDiagnosticOutboundFailure(&DiagnosticExchange{
		Flow:     flow,
		Sequence: 7,
		Started:  time.Now().Add(-time.Second),
	}, errors.New("dial tcp: connection refused"))
	flow.session.close(flow)

	path := filepath.Join(captureDir, "channel", "2026-07-24", "transport-failure-trace", "request-log.json")
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(filepath.Dir(path), ".capture-complete"))
		return err == nil
	}, time.Second, 10*time.Millisecond)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var combined diagnosticCombinedCPAJSON
	require.NoError(t, common.Unmarshal(data, &combined))
	require.Len(t, combined.APIResponses, 1)
	require.Equal(t, int64(7), combined.APIResponses[0].Sequence)
	require.Equal(t, "dial tcp: connection refused", combined.APIResponses[0].Error)
	require.Equal(t, "empty", combined.APIResponses[0].Response.Body.Encoding)
}

func TestDiagnosticCaptureOutboundResponseIncludesDuration(t *testing.T) {
	flow := &DiagnosticFlow{Started: time.Now()}
	content := buildDiagnosticCPAJSON(
		DiagnosticCaptureConfig{Mode: "metadata"},
		flow,
		11,
		"outbound",
		"response",
		map[string]any{"status_code": 200, "duration_ms": int64(3194)},
		captureBody{},
	)
	require.NotNil(t, content.APIResponse)
	require.Equal(t, int64(3194), content.APIResponse.DurationMS)
}

func TestDiagnosticRetrySummaryGroupsAttemptsBySequence(t *testing.T) {
	requests := []*diagnosticCapturePartState{
		{sequence: 10, meta: map[string]any{"channel_id": 137, "channel_name": "first", "model_name": "model-a", "upstream_request_id": "up-1"}},
		{sequence: 20, meta: map[string]any{"channel_id": 135, "channel_name": "second", "model_name": "model-a", "upstream_request_id": "up-2"}},
	}
	responses := []*diagnosticCapturePartState{
		{sequence: 10, meta: map[string]any{"status_code": 429, "duration_ms": int64(12), "error": "rate limited"}},
		{sequence: 20, meta: map[string]any{"status_code": 200, "duration_ms": int64(3194), "first_response_ms": int64(1500)}},
	}
	summary := buildDiagnosticRetrySummary(1, requests, responses)
	require.NotNil(t, summary)
	require.Len(t, summary.Attempts, 2)
	require.Equal(t, "failed", summary.Attempts[0].Status)
	require.Equal(t, "success", summary.Attempts[1].Status)
	require.Equal(t, 135, summary.FinalChannelID)
	require.Equal(t, "up-2", summary.FinalUpstreamRequestID)
}

func TestDiagnosticRetryGroupUsesDownstreamRequestID(t *testing.T) {
	flow := &DiagnosticFlow{Context: diagnosticRequestContextJSON{RequestID: "downstream-1", RetryCount: 1}}
	content := buildDiagnosticCPAJSON(
		DiagnosticCaptureConfig{Mode: "metadata"},
		flow,
		10,
		"outbound",
		"request",
		map[string]any{"url": "https://upstream.example", "method": "POST"},
		captureBody{},
	)
	require.NotNil(t, content.APIRequest)
	require.Equal(t, "downstream-1", content.APIRequest.RetryGroup)
}

func TestDiagnosticCaptureWebSocketFailureIsRecorded(t *testing.T) {
	captureDir := t.TempDir()
	flow := &DiagnosticFlow{
		TraceID: "websocket-failure-trace",
		Channel: "channel",
		Started: time.Date(2026, 7, 24, 10, 0, 0, 0, time.Local),
	}
	flow.session = newDiagnosticCaptureSession(DiagnosticCaptureConfig{
		Enabled:    true,
		Mode:       "full",
		CaptureDir: captureDir,
		TempDir:    filepath.Join(t.TempDir(), "temp"),
	}, flow)
	require.NotNil(t, flow.session)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("diagnostic_flow", flow)
	RecordDiagnosticWebSocketFailure(c, "wss://upstream.example/realtime", errors.New("broken pipe"))
	flow.session.close(flow)

	path := filepath.Join(captureDir, "channel", "2026-07-24", "websocket-failure-trace", "request-log.json")
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, time.Second, 10*time.Millisecond)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var combined diagnosticCombinedCPAJSON
	require.NoError(t, common.Unmarshal(data, &combined))
	require.Len(t, combined.APIResponses, 1)
	require.Equal(t, "broken pipe", combined.APIResponses[0].Error)
	require.Equal(t, "empty", combined.APIResponses[0].Response.Body.Encoding)
}

func TestDiagnosticCaptureWebSocketFramesUseGroupedSpoolFiles(t *testing.T) {
	root := t.TempDir()
	captureDir := filepath.Join(root, "captures")
	tempDir := filepath.Join(root, "capture-temp")
	flow := &DiagnosticFlow{
		TraceID: "websocket-frame-trace",
		Channel: "channel",
		Started: time.Date(2026, 7, 24, 10, 0, 0, 0, time.Local),
	}
	flow.session = newDiagnosticCaptureSession(DiagnosticCaptureConfig{
		Enabled:    true,
		Mode:       "full",
		CaptureDir: captureDir,
		TempDir:    tempDir,
	}, flow)
	require.NotNil(t, flow.session)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("diagnostic_flow", flow)
	RecordDiagnosticWebSocketFrame(c, "wss://upstream.example/realtime", "request", []byte(`{"type":"start"}`))
	RecordDiagnosticWebSocketFrame(c, "wss://upstream.example/realtime", "request", nil)
	RecordDiagnosticWebSocketFrame(c, "wss://upstream.example/realtime", "response", []byte(`{"type":"delta"}`))
	RecordDiagnosticWebSocketFrame(c, "wss://upstream.example/realtime", "response", []byte{0x01, 0x02, 0x03})
	flow.session.close(flow)

	path := filepath.Join(captureDir, "channel", "2026-07-24", "websocket-frame-trace", "request-log.json")
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(filepath.Dir(path), ".capture-complete"))
		return err == nil
	}, time.Second, 10*time.Millisecond)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var combined diagnosticCombinedCPAJSON
	require.NoError(t, common.Unmarshal(data, &combined))
	require.Len(t, combined.APIRequests, 1)
	require.Len(t, combined.APIRequests[0].WebSocketFrames, 2)
	require.Len(t, combined.APIResponses, 1)
	require.Len(t, combined.APIResponses[0].WebSocketFrames, 2)

	decoded, err := base64.StdEncoding.DecodeString(combined.APIRequests[0].WebSocketFrames[0].Body.Base64)
	require.NoError(t, err)
	require.Equal(t, []byte(`{"type":"start"}`), decoded)
	require.Empty(t, combined.APIRequests[0].WebSocketFrames[1].Body.Base64)
	decoded, err = base64.StdEncoding.DecodeString(combined.APIResponses[0].WebSocketFrames[1].Body.Base64)
	require.NoError(t, err)
	require.Equal(t, []byte{0x01, 0x02, 0x03}, decoded)

	requestDir := filepath.Join(tempDir, "2026-07-24", "websocket-frame-trace")
	require.Eventually(t, func() bool {
		_, err := os.Stat(requestDir)
		return os.IsNotExist(err)
	}, time.Second, 10*time.Millisecond)
}

func TestDiagnosticCaptureWebSocketFrameRestoresSegmentsAcrossSpoolFiles(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.part")
	secondPath := filepath.Join(root, "second.part")
	writeSegment := func(path string, payload []byte, flags uint64) {
		file, err := os.Create(path)
		require.NoError(t, err)
		defer file.Close()
		var header [diagnosticWebSocketFrameHeaderBytes]byte
		binary.BigEndian.PutUint64(header[0:8], 7)
		binary.BigEndian.PutUint64(header[8:16], uint64(time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC).UnixNano()))
		binary.BigEndian.PutUint64(header[16:24], 5)
		binary.BigEndian.PutUint64(header[24:32], uint64(len(payload)))
		binary.BigEndian.PutUint64(header[32:40], flags)
		_, err = file.Write(header[:])
		require.NoError(t, err)
		_, err = file.Write(payload)
		require.NoError(t, err)
	}
	writeSegment(firstPath, []byte("he"), diagnosticWebSocketFrameStart)
	writeSegment(secondPath, []byte("llo"), diagnosticWebSocketFrameEnd)

	flow := &DiagnosticFlow{TraceID: "segmented-websocket", Channel: "channel", Started: time.Date(2026, 7, 25, 12, 0, 0, 0, time.Local)}
	state := &diagnosticCapturePartState{
		partID:          "websocket-request-000007",
		sequence:        7,
		role:            "outbound",
		part:            "request",
		complete:        true,
		webSocketFrames: true,
		tempPath:        firstPath,
		tempPaths:       []string{firstPath, secondPath},
		meta: map[string]any{
			"captured_at":            "2026-07-25T12:00:00Z",
			"upstream_url":           "wss://upstream.example/realtime",
			"websocket_frames":       true,
			"websocket_frame_format": diagnosticWebSocketFrameFormat,
		},
	}
	cfg := DiagnosticCaptureConfig{Enabled: true, Mode: "full", CaptureDir: filepath.Join(root, "captures")}
	require.NoError(t, writeDiagnosticCaptureSession(cfg, flow, []*diagnosticCapturePartState{state}))

	data, err := os.ReadFile(filepath.Join(cfg.CaptureDir, "channel", "2026-07-25", "segmented-websocket", "request-log.json"))
	require.NoError(t, err)
	var combined diagnosticCombinedCPAJSON
	require.NoError(t, common.Unmarshal(data, &combined))
	require.Len(t, combined.APIRequests, 1)
	require.Len(t, combined.APIRequests[0].WebSocketFrames, 1)
	decoded, err := base64.StdEncoding.DecodeString(combined.APIRequests[0].WebSocketFrames[0].Body.Base64)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), decoded)
}

func TestDiagnosticCaptureFailureIsRetriedWithoutDiscardingBody(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "temp")
	require.NoError(t, os.MkdirAll(tempDir, 0o700))
	tempFile, err := os.CreateTemp(tempDir, "request-*.part")
	require.NoError(t, err)
	_, err = tempFile.WriteString(`{"message":"retry"}`)
	require.NoError(t, err)
	require.NoError(t, tempFile.Close())

	cfg := DiagnosticCaptureConfig{
		Enabled:    true,
		Mode:       "full",
		CaptureDir: filepath.Join(root, "captures"),
		TempDir:    tempDir,
	}
	cfg.FailureDir = diagnosticCaptureFailureDir(cfg.CaptureDir)
	flow := &DiagnosticFlow{
		TraceID: "retry-trace",
		Channel: "channel",
		Started: time.Date(2026, 7, 23, 10, 0, 0, 0, time.Local),
	}
	state := &diagnosticCapturePartState{
		partID:       "inbound-request",
		role:         "inbound",
		part:         "request",
		meta:         map[string]any{"method": "POST", "path": "/v1/chat/completions"},
		tempPath:     tempFile.Name(),
		originalSize: int64(len(`{"message":"retry"}`)),
		savedSize:    int64(len(`{"message":"retry"}`)),
		complete:     true,
	}

	require.NoError(t, persistDiagnosticCaptureFailure(cfg, flow, []*diagnosticCapturePartState{state}, "forced write failure", true))
	require.NoFileExists(t, tempFile.Name())
	failurePath := diagnosticCaptureFailureRecordPath(cfg, flow)
	require.FileExists(t, failurePath)

	retryDiagnosticCaptureFailures(cfg)
	require.FileExists(t, filepath.Join(cfg.CaptureDir, "channel", "2026-07-23", "retry-trace", "request-log.json"))
	require.NoDirExists(t, filepath.Dir(failurePath))
}

func TestDiagnosticCaptureRetrySkipsArchivedFailures(t *testing.T) {
	root := t.TempDir()
	cfg := DiagnosticCaptureConfig{
		Enabled:    true,
		Mode:       "full",
		CaptureDir: filepath.Join(root, "captures"),
		TempDir:    filepath.Join(root, "temp"),
	}
	cfg.FailureDir = diagnosticCaptureFailureDir(cfg.CaptureDir)
	flow := &DiagnosticFlow{
		TraceID: "archived-trace",
		Channel: "channel",
		Started: time.Date(2026, 7, 23, 10, 0, 0, 0, time.Local),
	}
	failurePath := filepath.Join(
		cfg.FailureDir,
		flow.Channel,
		flow.Started.Format("2006-01-02"),
		".abandoned-"+flow.TraceID,
		"capture-failure.json",
	)
	require.NoError(t, os.MkdirAll(filepath.Dir(failurePath), 0o700))
	require.NoError(t, writeDiagnosticCaptureFailureRecord(failurePath, diagnosticCaptureFailureRecord{
		TraceID:   flow.TraceID,
		Channel:   flow.Channel,
		StartedAt: flow.Started.UnixNano(),
		Retryable: true,
	}))

	retryDiagnosticCaptureFailures(cfg)

	require.FileExists(t, failurePath)
	require.NoFileExists(t, filepath.Join(cfg.CaptureDir, flow.Channel, "2026-07-23", flow.TraceID, "request-log.json"))
}

func TestDiagnosticCaptureRetryRestoresFragmentFromRequestScopedTempDirectory(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "temp")
	started := time.Date(2026, 7, 23, 10, 0, 0, 0, time.Local)
	flow := &DiagnosticFlow{TraceID: "restore-trace", Channel: "channel", Started: started}
	requestTempDir := filepath.Join(tempDir, "2026-07-23", "restore-trace")
	require.NoError(t, os.MkdirAll(requestTempDir, 0o700))
	tempPartName := "inbound-request-recover.part"
	tempPartPath := filepath.Join(requestTempDir, tempPartName)
	body := []byte(`{"message":"restore"}`)
	require.NoError(t, os.WriteFile(tempPartPath, body, 0o600))

	cfg := DiagnosticCaptureConfig{
		Enabled:    true,
		Mode:       "full",
		CaptureDir: filepath.Join(root, "captures"),
		TempDir:    tempDir,
	}
	cfg.FailureDir = diagnosticCaptureFailureDir(cfg.CaptureDir)
	failurePath := diagnosticCaptureFailureRecordPath(cfg, flow)
	require.NoError(t, os.MkdirAll(filepath.Dir(failurePath), 0o700))
	require.NoError(t, writeDiagnosticCaptureFailureRecord(failurePath, diagnosticCaptureFailureRecord{
		TraceID:       flow.TraceID,
		Channel:       flow.Channel,
		StartedAt:     flow.Started.UnixNano(),
		Retryable:     true,
		LastError:     "interrupted while moving fragments",
		LastAttemptAt: time.Now().UnixNano(),
		Parts: []diagnosticCaptureFailurePart{{
			PartID:       "inbound-request",
			Role:         "inbound",
			Part:         "request",
			FileName:     "000.part",
			TempFileName: tempPartName,
			SavedSize:    int64(len(body)),
			OriginalSize: int64(len(body)),
			Complete:     true,
		}},
	}))

	retryDiagnosticCaptureFailures(cfg)
	require.FileExists(t, filepath.Join(cfg.CaptureDir, "channel", "2026-07-23", "restore-trace", "request-log.json"))
	require.NoFileExists(t, tempPartPath)
	require.NoDirExists(t, filepath.Dir(failurePath))
}

func TestDiagnosticCaptureFailureTempPartPathKeepsFragmentActive(t *testing.T) {
	tempDir := t.TempDir()
	flow := &DiagnosticFlow{
		TraceID: "active-retry-trace",
		Started: time.Date(2026, 7, 23, 10, 0, 0, 0, time.Local),
	}
	partPath := filepath.Join(tempDir, "2026-07-23", "active-retry-trace", "inbound-request-recover.part")
	require.NoError(t, os.MkdirAll(filepath.Dir(partPath), 0o700))
	require.NoError(t, os.WriteFile(partPath, []byte("body"), 0o600))

	path := diagnosticCaptureFailureTempPartPath(DiagnosticCaptureConfig{TempDir: tempDir}, flow, diagnosticCaptureFailurePart{
		PartID:       "inbound-request",
		TempFileName: filepath.Base(partPath),
	})
	require.Equal(t, partPath, path)
	require.True(t, isDiagnosticTempFileActive(path))
	t.Cleanup(func() { markDiagnosticTempFileInactive(path) })
}

func TestDiagnosticCaptureFailureTempPartPathDoesNotSubstituteNamedFragment(t *testing.T) {
	tempDir := t.TempDir()
	flow := &DiagnosticFlow{
		TraceID: "missing-fragment-trace",
		Started: time.Date(2026, 7, 23, 10, 0, 0, 0, time.Local),
	}
	dir := filepath.Join(tempDir, "2026-07-23", "missing-fragment-trace")
	replacement := filepath.Join(dir, "websocket-request-000001-other.part")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(replacement, []byte("another segment"), 0o600))

	path := diagnosticCaptureFailureTempPartPath(DiagnosticCaptureConfig{TempDir: tempDir}, flow, diagnosticCaptureFailurePart{
		PartID:       "websocket-request-000001",
		TempFileName: "websocket-request-000001-missing.part",
	})

	require.Empty(t, path)
	require.False(t, isDiagnosticTempFileActive(replacement))
}

func TestDiagnosticCaptureRateDurationAvoidsDurationOverflow(t *testing.T) {
	duration := diagnosticCaptureRateDuration(int64(10)<<40, 1024*1024)
	require.Equal(t, time.Duration(10*1024*1024)*time.Second, duration)
}

func TestDiagnosticCaptureMaintenanceErrorLimiterBoundsRepeatedErrors(t *testing.T) {
	var limiter diagnosticCaptureMaintenanceErrorLimiter
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	require.True(t, limiter.shouldLog(diagnosticCaptureMaintenanceScanError, now))
	require.False(t, limiter.shouldLog(diagnosticCaptureMaintenanceScanError, now.Add(4*time.Minute+59*time.Second)))
	require.True(t, limiter.shouldLog(diagnosticCaptureMaintenanceDirectoryListError, now.Add(time.Minute)))
	require.True(t, limiter.shouldLog(diagnosticCaptureMaintenanceScanError, now.Add(diagnosticCaptureMaintenanceErrorLogInterval)))
}

func TestCleanupDiagnosticCaptureTempFilesKeepsActiveFiles(t *testing.T) {
	tempDir := t.TempDir()
	stalePath := filepath.Join(tempDir, "stale.part")
	activePath := filepath.Join(tempDir, "active.part")
	require.NoError(t, os.WriteFile(stalePath, []byte("stale"), 0o600))
	require.NoError(t, os.WriteFile(activePath, []byte("active"), 0o600))
	markDiagnosticTempFileActive(activePath)
	t.Cleanup(func() { markDiagnosticTempFileInactive(activePath) })

	cleanupDiagnosticCaptureTempFiles(tempDir, time.Now().Add(time.Hour))

	_, err := os.Stat(stalePath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(activePath)
	require.NoError(t, err)
}

func TestCleanupDiagnosticCaptureTempFilesKeepsActiveRequestDirectory(t *testing.T) {
	tempRoot := t.TempDir()
	spoolDir := filepath.Join(tempRoot, "2026-07-24", "active-request")
	require.NoError(t, os.MkdirAll(spoolDir, 0o700))
	markDiagnosticTempFileActive(spoolDir)
	t.Cleanup(func() { markDiagnosticTempFileInactive(spoolDir) })

	cleanupDiagnosticCaptureTempFiles(tempRoot, time.Now())

	require.DirExists(t, spoolDir)
}

func TestRemoveDiagnosticCaptureTempFileRemovesEmptySpoolDirectories(t *testing.T) {
	tempRoot := t.TempDir()
	spoolDir := filepath.Join(tempRoot, "2026-07-24", "request-id")
	require.NoError(t, os.MkdirAll(spoolDir, 0o700))
	tempPath := filepath.Join(spoolDir, "inbound-request.part")
	require.NoError(t, os.WriteFile(tempPath, []byte("body"), 0o600))

	removeDiagnosticCaptureTempFile(DiagnosticCaptureConfig{TempDir: tempRoot}, &diagnosticCapturePartState{
		tempPath:  tempPath,
		savedSize: int64(len("body")),
	})

	require.NoDirExists(t, spoolDir)
	require.DirExists(t, tempRoot)
}

func TestMoveDiagnosticCaptureFragmentRemovesSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "capture-temp", "part.part")
	destination := filepath.Join(root, "diagnostic-capture-failures", "000.part")
	require.NoError(t, os.MkdirAll(filepath.Dir(source), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o700))
	require.NoError(t, os.WriteFile(source, []byte("captured body"), 0o600))

	require.NoError(t, moveDiagnosticCaptureFragment(source, destination))
	_, err := os.Stat(source)
	require.True(t, os.IsNotExist(err))
	body, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, []byte("captured body"), body)
}

func TestDiagnosticCaptureChannelEnabledReturnsFalseWhenChannelLookupFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	require.False(t, diagnosticCaptureChannelEnabled(c, 999999))
}

func TestValidateDiagnosticCaptureDirectoriesRejectsOverlaps(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name       string
		captureDir string
		tempDir    string
	}{
		{name: "same directory", captureDir: filepath.Join(root, "captures"), tempDir: filepath.Join(root, "captures")},
		{name: "temporary inside formal", captureDir: filepath.Join(root, "captures"), tempDir: filepath.Join(root, "captures", "temp")},
		{name: "formal inside temporary", captureDir: filepath.Join(root, "temp", "captures"), tempDir: filepath.Join(root, "temp")},
		{name: "temporary equals failure", captureDir: filepath.Join(root, "captures"), tempDir: filepath.Join(root, "diagnostic-capture-failures")},
		{name: "formal equals derived failure", captureDir: filepath.Join(root, "diagnostic-capture-failures"), tempDir: filepath.Join(root, "temp")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Error(t, ValidateDiagnosticCaptureDirectories(test.captureDir, test.tempDir))
		})
	}
}

func TestValidateDiagnosticCaptureDirectoriesAllowsSiblingDirectories(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, ValidateDiagnosticCaptureDirectories(
		filepath.Join(root, "captures"),
		filepath.Join(root, "capture-temp"),
	))
}

func TestDiagnosticCaptureChannelEnabledRespectsChannelSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	channelID := 123456

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	channelInfoEnabled := false
	channel := &model.Channel{
		Id: channelID,
		ChannelInfo: model.ChannelInfo{
			DiagnosticCaptureEnabled: &channelInfoEnabled,
		},
	}

	model.InitChannelCache()
	model.CacheUpdateChannel(channel)

	require.False(t, diagnosticCaptureChannelEnabled(c, channelID))

	channelInfoEnabled = true
	channel.ChannelInfo.DiagnosticCaptureEnabled = &channelInfoEnabled
	model.CacheUpdateChannel(channel)

	require.True(t, diagnosticCaptureChannelEnabled(c, channelID))
}
