package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const diagnosticCaptureUsageFilename = ".diagnostic-capture-usage.json"

type diagnosticCaptureUsage struct {
	Version          int    `json:"version"`
	StorageID        string `json:"storage_id"`
	Scope            string `json:"scope"`
	TotalBytes       int64  `json:"total_bytes"`
	TemporaryBytes   int64  `json:"temporary_bytes"`
	SavedAt          int64  `json:"saved_at"`
	ReconciledAt     int64  `json:"reconciled_at"`
	CleanShutdown    bool   `json:"clean_shutdown"`
	Estimated        bool   `json:"estimated"`
	UncertaintyBytes int64  `json:"uncertainty_bytes"`
}

func (usage diagnosticCaptureUsage) validate() error {
	if len(usage.Scope) != 64 || strings.Trim(usage.Scope, "0123456789abcdef") != "" {
		return errors.New("invalid diagnostic capture storage scope")
	}
	if len(usage.StorageID) != 32 || strings.Trim(usage.StorageID, "0123456789abcdef") != "" {
		return errors.New("invalid diagnostic capture storage identity")
	}
	if usage.Version != 2 || usage.TotalBytes < 0 || usage.TemporaryBytes < 0 || usage.TemporaryBytes > usage.TotalBytes || usage.SavedAt <= 0 || usage.UncertaintyBytes < 0 {
		return errors.New("invalid diagnostic capture usage checkpoint")
	}
	return nil
}

// Loading is background-only. A bounded read also rejects an accidentally
// replaced statistics file without loading arbitrary capture bodies into RAM.
func loadDiagnosticCaptureUsage(path, scope string) (*diagnosticCaptureUsage, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	const maxCheckpointBytes = 64 * 1024
	data, err := io.ReadAll(io.LimitReader(file, maxCheckpointBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCheckpointBytes {
		return nil, errors.New("diagnostic capture usage checkpoint is too large")
	}
	var usage diagnosticCaptureUsage
	if err := common.Unmarshal(data, &usage); err != nil {
		return nil, err
	}
	if err := usage.validate(); err != nil {
		return nil, err
	}
	if usage.Scope != scope {
		return nil, errors.New("diagnostic capture usage belongs to another storage scope")
	}
	return &usage, nil
}

// A single background worker replaces the checkpoint on the same filesystem.
// The live file is never truncated: failures before rename retain its previous
// complete contents. Context cancellation is checked between filesystem calls;
// it cannot interrupt an OS disk stall. No relay/capture writer waits on this I/O.
func saveDiagnosticCaptureUsage(ctx context.Context, path string, usage diagnosticCaptureUsage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := usage.validate(); err != nil {
		return err
	}
	// JSON does not support comments. Keep human-readable explanations in a
	// separate object; loading ignores it and only trusts the validated fields.
	document := struct {
		Notes map[string]string `json:"_说明"`
		diagnosticCaptureUsage
	}{
		Notes: map[string]string{
			"文件用途":              "自动保存诊断抓包容量统计；请勿手动修改数值。与抓包、临时目录同级，备份时一起保留。",
			"version":           "统计文件格式版本，不是程序版本。",
			"storage_id":        "本次存储统计的随机标识，正常重启保持不变；文件丢失或失效后重新生成。",
			"scope":             "节点名与抓包、临时、失败目录组合的哈希，用于校验统计适用范围。",
			"total_bytes":       "抓包、临时和失败目录的总占用，单位：字节；不包含本统计文件。",
			"temporary_bytes":   "临时抓包目录占用，单位：字节；已包含在 total_bytes 内，请勿重复相加。",
			"saved_at":          "本次保存时间，Unix 时间戳，单位：秒。",
			"reconciled_at":     "最近一次后台容量校准时间，Unix 时间戳（秒）；小于等于 0 表示尚未校准。",
			"clean_shutdown":    "true 表示退出时已完成抓包收尾并保存统计；false 表示运行中或未确认正常退出，重启时读取有效统计，误差由定时校准修正。",
			"estimated":         "true 表示当前容量为估算值，可能存在异常退出、外部文件操作或读盘失败造成的误差。",
			"uncertainty_bytes": "旧格式兼容字段，当前为 0；异常重启后的统计标为估算值，由定时校准修正。",
		},
		diagnosticCaptureUsage: usage,
	}
	data, err := common.Marshal(document)
	if err != nil {
		return err
	}
	data, err = common.IndentJson(data)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, diagnosticCaptureUsageFilename+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	// Linux deployment: persist the directory entry as well as the contents.
	// Windows does not support os.File.Sync on an opened directory.
	if runtime.GOOS != "windows" {
		parent, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer parent.Close()
		return parent.Sync()
	}
	return nil
}
