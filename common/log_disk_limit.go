package common

import (
	"os"
	"path/filepath"
	"sort"
)

const bytesPerMB = 1024 * 1024

type LogPruneResult struct {
	DeletedCount int
	FreedBytes   int64
	TotalBytes   int64
	FailedFiles  []string
}

type LogFileMatcher func(path string, info os.FileInfo) bool

func MaxSizeMBToBytes(maxSizeMB int) int64 {
	if maxSizeMB <= 0 {
		return 0
	}
	return int64(maxSizeMB) * bytesPerMB
}

func PruneLogFilesByMaxSize(root string, maxBytes int64, matcher LogFileMatcher, exclude map[string]struct{}) (LogPruneResult, error) {
	result := LogPruneResult{}
	if root == "" || maxBytes <= 0 {
		return result, nil
	}

	type candidate struct {
		path    string
		size    int64
		modTime int64
	}
	var candidates []candidate
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if matcher != nil && !matcher(path, info) {
			return nil
		}
		result.TotalBytes += info.Size()
		if _, ok := exclude[path]; ok {
			return nil
		}
		candidates = append(candidates, candidate{
			path:    path,
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
		})
		return nil
	})
	if err != nil {
		return result, err
	}
	if result.TotalBytes <= maxBytes {
		return result, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modTime == candidates[j].modTime {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].modTime < candidates[j].modTime
	})

	currentBytes := result.TotalBytes
	for _, file := range candidates {
		if currentBytes <= maxBytes {
			break
		}
		if err := os.Remove(file.path); err != nil {
			result.FailedFiles = append(result.FailedFiles, file.path)
			continue
		}
		result.DeletedCount++
		result.FreedBytes += file.size
		currentBytes -= file.size
	}
	return result, nil
}
