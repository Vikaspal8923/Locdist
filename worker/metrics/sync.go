package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"google.golang.org/protobuf/proto"
)

var writeMu sync.Mutex

func AppendJSONL(path string, event map[string]any) {
	if path == "" {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(data, '\n'))
}

func SyncMetricsPath(workspaceRoot, jobID, filename string) string {
	if workspaceRoot == "" || jobID == "" {
		return ""
	}
	return filepath.Join(workspaceRoot, jobID, "logs", filename)
}

func ProtoBytes(message proto.Message) int {
	if message == nil {
		return 0
	}
	return proto.Size(message)
}

func EstimatedLinkMbps() float64 {
	value := os.Getenv("LDGCC_ESTIMATED_LINK_MBPS")
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func EstimateTransferMS(totalBytes int, linkMbps float64) float64 {
	if totalBytes <= 0 || linkMbps <= 0 {
		return 0
	}
	return (float64(totalBytes) * 8.0 * 1000.0) / (linkMbps * 1_000_000.0)
}
