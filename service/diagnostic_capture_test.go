package service

import (
	"bufio"
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

func TestDiagnosticCaptureChannelDefaultIsDisabled(t *testing.T) {
	require.False(t, (model.ChannelInfo{}).IsDiagnosticCaptureEnabled())
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
	tempDir := filepath.Join(root, "temporary")
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
	tempDir := filepath.Join(root, "temporary")
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
		DiagnosticCaptureTempDirKey:            tempDir,
		DiagnosticCaptureMaxStorageBytesKey:    "1",
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
	require.NotNil(t, combined.RequestBody)
	require.Equal(t, "json", combined.RequestBody.Encoding)
	require.Empty(t, combined.RequestBody.Text)
	require.Equal(t, map[string]any{"message": "complete"}, combined.RequestBody.JSON)
	require.Contains(t, string(data), "\n  \"request_body\": {")
	require.Contains(t, string(data), "\"original_size\": 22")
	require.Contains(t, string(data), "\"truncated\": false")
	require.NotContains(t, string(data), `"text"`)
	require.NotContains(t, string(data), `"body_original_size"`)
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 2)
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

func TestDiagnosticCaptureRateDurationAvoidsDurationOverflow(t *testing.T) {
	duration := diagnosticCaptureRateDuration(int64(10)<<40, 1024*1024)
	require.Equal(t, time.Duration(10*1024*1024)*time.Second, duration)
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
