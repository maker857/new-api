package common

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneLogFilesByMaxSizeDeletesOldestFiles(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeSizedTestLogFile(t, filepath.Join(dir, "old.log"), 60)
	middlePath := writeSizedTestLogFile(t, filepath.Join(dir, "middle.log"), 60)
	newPath := writeSizedTestLogFile(t, filepath.Join(dir, "new.log"), 60)
	require.NoError(t, os.Chtimes(oldPath, time.Now().Add(-3*time.Hour), time.Now().Add(-3*time.Hour)))
	require.NoError(t, os.Chtimes(middlePath, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))
	require.NoError(t, os.Chtimes(newPath, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)))

	result, err := PruneLogFilesByMaxSize(dir, 120, func(_ string, _ os.FileInfo) bool {
		return true
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, 1, result.DeletedCount)
	assert.Equal(t, int64(60), result.FreedBytes)
	assert.NoFileExists(t, oldPath)
	assert.FileExists(t, middlePath)
	assert.FileExists(t, newPath)
}

func TestPruneLogFilesByMaxSizeSkipsExcludedFiles(t *testing.T) {
	dir := t.TempDir()
	activePath := writeSizedTestLogFile(t, filepath.Join(dir, "active.log"), 80)
	oldPath := writeSizedTestLogFile(t, filepath.Join(dir, "old.log"), 80)
	require.NoError(t, os.Chtimes(activePath, time.Now().Add(-3*time.Hour), time.Now().Add(-3*time.Hour)))
	require.NoError(t, os.Chtimes(oldPath, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))

	result, err := PruneLogFilesByMaxSize(dir, 80, func(_ string, _ os.FileInfo) bool {
		return true
	}, map[string]struct{}{activePath: {}})

	require.NoError(t, err)
	assert.Equal(t, 1, result.DeletedCount)
	assert.Equal(t, int64(80), result.FreedBytes)
	assert.FileExists(t, activePath)
	assert.NoFileExists(t, oldPath)
}

func writeSizedTestLogFile(t *testing.T, path string, size int) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0644))
	return path
}
