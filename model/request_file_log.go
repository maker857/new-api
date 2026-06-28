package model

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

const requestLogPathOtherKey = "request_log_path"

func writeRequestFileLog(c *gin.Context, log *Log) {
	if log != nil {
		ensureLogRequestId(log)
	}
	if c == nil || log == nil || log.RequestId == "" || *common.LogDir == "" || !requestFileLogEnabled(c) {
		return
	}
	path, err := createRequestFileLog(c, log)
	if err != nil {
		common.SysError("failed to save request log file: " + err.Error())
		return
	}
	other, _ := common.StrToMap(log.Other)
	if other == nil {
		other = map[string]interface{}{}
	}
	other[requestLogPathOtherKey] = path
	log.Other = common.MapToJsonStr(other)
}

func requestFileLogEnabled(c *gin.Context) bool {
	settings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	if !ok {
		return true
	}
	return settings.SaveRequestLog
}

func createRequestFileLog(c *gin.Context, log *Log) (string, error) {
	createdAt := time.Unix(log.CreatedAt, 0)
	if log.CreatedAt == 0 {
		createdAt = time.Now()
	}
	dir := filepath.Join(*common.LogDir, requestFileLogChannelDir(c, log), createdAt.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	filePath := filepath.Join(dir, requestFileLogName(createdAt, log.RequestId))
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}

	if err := writeRequestInfoSection(file, c, log, createdAt); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := writeRequestBodySection(file, c); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := writeAPIRequestSection(file, log); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := writeAPIResponseSection(file, log); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := writeResponseSection(file, c, log); err != nil {
		_ = file.Close()
		return "", err
	}
	if closeErr := file.Close(); closeErr != nil {
		common.SysError("failed to close request log file: " + closeErr.Error())
	}
	if err := pruneRequestLogFilesByMaxSize(filePath); err != nil {
		common.SysError("failed to prune request log files: " + err.Error())
	}
	return filePath, nil
}

func pruneRequestLogFilesByMaxSize(currentPath string) error {
	maxBytes := common.MaxSizeMBToBytes(common.RequestLogMaxSizeMB)
	if *common.LogDir == "" || maxBytes <= 0 {
		return nil
	}
	_, err := common.PruneLogFilesByMaxSize(*common.LogDir, maxBytes, func(path string, info os.FileInfo) bool {
		name := filepath.Base(path)
		if strings.HasPrefix(name, "oneapi-") && strings.HasSuffix(name, ".log") {
			return false
		}
		return strings.HasSuffix(name, ".log")
	}, map[string]struct{}{currentPath: {}})
	return err
}

func requestFileLogName(createdAt time.Time, requestId string) string {
	return fmt.Sprintf("%s-%s.log", createdAt.Format("150405.000000000"), sanitizeRequestLogFilePart(requestId))
}

func requestFileLogChannelDir(c *gin.Context, log *Log) string {
	channelName := common.GetContextKeyString(c, constant.ContextKeyChannelName)
	if strings.TrimSpace(channelName) == "" && log != nil {
		channelName = log.ChannelName
	}
	if strings.TrimSpace(channelName) == "" && log != nil {
		other, _ := common.StrToMap(log.Other)
		if other != nil {
			if otherChannelName, ok := other["channel_name"].(string); ok {
				channelName = otherChannelName
			}
		}
	}
	if strings.TrimSpace(channelName) != "" {
		if dir := sanitizeRequestLogDirPart(channelName); dir != "" {
			return dir
		}
	}
	if log != nil && log.ChannelId > 0 {
		return fmt.Sprintf("channel-%d", log.ChannelId)
	}
	return "unknown-channel"
}

func sanitizeRequestLogDirPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range value {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			builder.WriteByte('-')
		default:
			if r < 32 {
				builder.WriteByte('-')
				continue
			}
			builder.WriteRune(r)
		}
	}
	out := strings.TrimSpace(builder.String())
	out = strings.Trim(out, ".")
	if out == "" {
		return ""
	}
	return out
}

func sanitizeRequestLogFilePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "request"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	out := strings.Trim(builder.String(), "-_.")
	if out == "" {
		return "request"
	}
	return out
}

func writeRequestInfoSection(file *os.File, c *gin.Context, log *Log, createdAt time.Time) error {
	requestURL := ""
	method := ""
	if c.Request != nil {
		method = c.Request.Method
		if c.Request.URL != nil {
			requestURL = c.Request.URL.String()
		}
	}
	if requestURL == "" {
		requestURL = requestPathFromLog(log)
	}
	if method == "" {
		method = http.MethodPost
	}

	if _, err := fmt.Fprintln(file, "=== REQUEST INFO ==="); err != nil {
		return err
	}
	lines := []string{
		"Version: " + common.Version,
		"URL: " + requestURL,
		"Method: " + method,
		"Downstream Transport: http",
		"Upstream Transport: http",
		"Timestamp: " + createdAt.Format(time.RFC3339Nano),
		"Request ID: " + log.RequestId,
	}
	if log.UpstreamRequestId != "" {
		lines = append(lines, "Upstream Request ID: "+log.UpstreamRequestId)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(file, line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(file); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(file, "=== HEADERS ==="); err != nil {
		return err
	}
	if c.Request != nil {
		writeSortedHeaders(file, c.Request.Header)
	}
	_, err := fmt.Fprintln(file)
	return err
}

func requestPathFromLog(log *Log) string {
	other, _ := common.StrToMap(log.Other)
	if other == nil {
		return ""
	}
	if path, ok := other["request_path"].(string); ok {
		return path
	}
	return ""
}

func writeRequestBodySection(file *os.File, c *gin.Context) error {
	if _, err := fmt.Fprintln(file, "=== REQUEST BODY ==="); err != nil {
		return err
	}
	body, err := requestBodyBytes(c)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		if _, err := fmt.Fprint(file, "<empty>"); err != nil {
			return err
		}
	} else if _, err := file.Write(compactRequestLogBody(body)); err != nil {
		return err
	}
	if _, writeErr := fmt.Fprintln(file); writeErr != nil {
		return writeErr
	}
	_, writeErr := fmt.Fprintln(file)
	return writeErr
}

func requestBodyBytes(c *gin.Context) ([]byte, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, nil
	}
	body, err := common.GetRequestBody(c)
	if err != nil {
		return nil, err
	}
	reader, ok := body.(io.Reader)
	if !ok {
		return nil, fmt.Errorf("request body storage is not readable")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return data, nil
}

func compactRequestLogBody(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}
	var compacted bytes.Buffer
	if err := common.CompactJson(&compacted, trimmed); err != nil {
		return trimmed
	}
	return compacted.Bytes()
}

func writeAPIRequestSection(file *os.File, log *Log) error {
	if _, err := fmt.Fprintln(file, "=== API REQUEST ==="); err != nil {
		return err
	}
	lines := []string{
		"Model: " + log.ModelName,
		fmt.Sprintf("Channel ID: %d", log.ChannelId),
		"Group: " + log.Group,
		fmt.Sprintf("Stream: %t", log.IsStream),
		fmt.Sprintf("Prompt Tokens: %d", log.PromptTokens),
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(file, line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(file)
	return err
}

func writeAPIResponseSection(file *os.File, log *Log) error {
	if _, err := fmt.Fprintln(file, "=== API RESPONSE ==="); err != nil {
		return err
	}
	lines := []string{
		fmt.Sprintf("Status: %s", logStatusText(log.Type)),
		fmt.Sprintf("Completion Tokens: %d", log.CompletionTokens),
		fmt.Sprintf("Total Tokens: %d", log.PromptTokens+log.CompletionTokens),
		fmt.Sprintf("Use Time Seconds: %d", log.UseTime),
		fmt.Sprintf("Quota: %d", log.Quota),
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(file, line); err != nil {
			return err
		}
	}
	if strings.TrimSpace(log.Other) != "" && log.Other != "null" {
		if _, err := fmt.Fprintln(file); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(file, "Other:"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(file, log.Other); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(file)
	return err
}

func writeResponseSection(file *os.File, c *gin.Context, log *Log) error {
	if _, err := fmt.Fprintln(file, "=== RESPONSE ==="); err != nil {
		return err
	}
	if capture, ok := c.Get(common.KeyRequestLogResponseCapture); ok {
		if responseCapture, ok := capture.(*common.RequestLogResponseCapture); ok {
			status := c.Writer.Status()
			if status == 0 {
				status = http.StatusOK
			}
			return writeCapturedResponseSection(file, responseCapture.Snapshot(status))
		}
	}
	if log.Type == LogTypeError {
		if _, err := fmt.Fprintln(file, "Status: error"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(file, "Status: completed"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(file); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(file, "Body:"); err != nil {
		return err
	}
	content := strings.TrimSpace(log.Content)
	if content == "" {
		content = "<empty>"
	}
	_, err := fmt.Fprintln(file, content)
	return err
}

func writeCapturedResponseSection(file *os.File, response common.RequestLogResponseSnapshot) error {
	if response.Status == 0 {
		response.Status = http.StatusOK
	}
	if _, err := fmt.Fprintf(file, "Status: %d %s\n", response.Status, http.StatusText(response.Status)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(file); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(file, "Headers:"); err != nil {
		return err
	}
	writeSortedHeaders(file, response.Header)
	if _, err := fmt.Fprintln(file); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(file, "Body:"); err != nil {
		return err
	}
	body := bytes.TrimSpace(response.Body)
	if len(body) == 0 {
		if _, err := fmt.Fprintln(file, "<empty>"); err != nil {
			return err
		}
	} else if _, err := file.Write(compactRequestLogBody(body)); err != nil {
		return err
	} else if _, err := fmt.Fprintln(file); err != nil {
		return err
	}
	if response.Truncated {
		_, err := fmt.Fprintf(file, "\n<truncated after %d bytes>\n", response.MaxBodyBytes)
		return err
	}
	return nil
}

func logStatusText(logType int) string {
	if logType == LogTypeError {
		return "error"
	}
	return "completed"
}

func writeSortedHeaders(file *os.File, headers http.Header) {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range headers[key] {
			_, _ = fmt.Fprintf(file, "%s: %s\n", key, maskRequestLogHeader(key, value))
		}
	}
}

func maskRequestLogHeader(key string, value string) string {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	if lowerKey == "authorization" || lowerKey == "proxy-authorization" || lowerKey == "cookie" || lowerKey == "set-cookie" || strings.Contains(lowerKey, "api-key") || strings.Contains(lowerKey, "token") {
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return "***masked***"
	}
	return value
}
