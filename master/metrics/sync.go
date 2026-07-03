package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func ProtoBytes(message proto.Message) int {
	if message == nil {
		return 0
	}
	return proto.Size(message)
}
