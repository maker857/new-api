package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var (
	ErrDiagnosticCaptureNotFound = errors.New("diagnostic capture file not found")
)

func DiagnosticCaptureFilePath(requestID, channel string) (string, error) {
	requestID = strings.TrimSpace(requestID)
	channel = strings.TrimSpace(channel)
	if requestID == "" || channel == "" {
		return "", fmt.Errorf("invalid diagnostic capture reference")
	}
	log, err := model.GetLogByRequestID(requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrDiagnosticCaptureNotFound
	}
	if err != nil {
		return "", err
	}
	cfg := DiagnosticCaptureConfigFromOptions()
	if strings.TrimSpace(cfg.CaptureDir) == "" {
		return "", ErrDiagnosticCaptureNotFound
	}
	base := filepath.Clean(cfg.CaptureDir)
	date := time.Unix(log.CreatedAt, 0).Format("2006-01-02")
	path := filepath.Join(base, safeCaptureName(channel, "unknown"), date, safeTraceID(requestID), "request-log.json")
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid diagnostic capture path")
	}
	return path, nil
}

func OpenDiagnosticCaptureFile(requestID, channel string) (*os.File, os.FileInfo, error) {
	path, err := DiagnosticCaptureFilePath(requestID, channel)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, ErrDiagnosticCaptureNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

func StreamDiagnosticCapture(w http.ResponseWriter, r *http.Request, requestID, channel string, download bool) error {
	file, info, err := OpenDiagnosticCaptureFile(requestID, channel)
	if err != nil {
		return err
	}
	defer file.Close()
	if download {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.json\"", safeTraceID(requestID)))
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "request-log.json", info.ModTime(), file)
	return nil
}

func ReadDiagnosticCapturePreview(requestID, channel string) ([]byte, int64, error) {
	file, info, err := OpenDiagnosticCaptureFile(requestID, channel)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	return content, info.Size(), err
}
