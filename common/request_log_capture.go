package common

import (
	"net/http"
	"sync"
)

const KeyRequestLogResponseCapture = "key_request_log_response_capture"

type RequestLogResponseCapture struct {
	mu           sync.Mutex
	header       http.Header
	body         []byte
	truncated    bool
	maxBodyBytes int
}

type RequestLogResponseSnapshot struct {
	Status       int
	Header       http.Header
	Body         []byte
	Truncated    bool
	MaxBodyBytes int
}

func NewRequestLogResponseCapture(header http.Header, maxBodyBytes int) *RequestLogResponseCapture {
	return &RequestLogResponseCapture{
		header:       header,
		maxBodyBytes: maxBodyBytes,
	}
}

func (capture *RequestLogResponseCapture) AppendBody(body []byte) {
	if capture == nil || len(body) == 0 {
		return
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()

	remaining := capture.maxBodyBytes - len(capture.body)
	if remaining <= 0 {
		capture.truncated = true
		return
	}
	if len(body) > remaining {
		capture.body = append(capture.body, body[:remaining]...)
		capture.truncated = true
		return
	}
	capture.body = append(capture.body, body...)
}

func (capture *RequestLogResponseCapture) Snapshot(status int) RequestLogResponseSnapshot {
	if capture == nil {
		return RequestLogResponseSnapshot{Status: status}
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()

	body := make([]byte, len(capture.body))
	copy(body, capture.body)

	return RequestLogResponseSnapshot{
		Status:       status,
		Header:       capture.header.Clone(),
		Body:         body,
		Truncated:    capture.truncated,
		MaxBodyBytes: capture.maxBodyBytes,
	}
}
