package results

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

const (
	MaxFileBytes  = 64 << 20
	MaxTotalBytes = 256 << 20
)

type jobConfig struct {
	Outputs []string `json:"outputs"`
}

type Manager struct{ workspace *workspace.Manager }

func New(workspaceManager *workspace.Manager) *Manager { return &Manager{workspace: workspaceManager} }

func (m *Manager) Manifest(jobID string) ([]*gradient.ResultFile, []string, []string, error) {
	root, err := m.workspace.Path(jobID)
	if err != nil {
		return nil, nil, nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, "job_config.json"))
	if err != nil {
		return nil, nil, nil, err
	}
	var config jobConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, nil, nil, fmt.Errorf("parse job_config.json: %w", err)
	}
	files := make(map[string]*gradient.ResultFile)
	missing := make([]string, 0)
	collectionErrors := make([]string, 0)
	var total int64
	add := func(relative string, logFile bool) error {
		if !safeRelative(relative) {
			return fmt.Errorf("unsafe result path %q", relative)
		}
		normalized := filepath.ToSlash(filepath.Clean(relative))
		if _, exists := files[normalized]; exists {
			return nil
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := rejectSymlinkComponents(root, normalized); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("result %q is not a regular file", relative)
		}
		if info.Size() > MaxFileBytes {
			return fmt.Errorf("result %q exceeds file size limit", relative)
		}
		total += info.Size()
		if total > MaxTotalBytes {
			return fmt.Errorf("results exceed total size limit")
		}
		digest, err := checksum(path)
		if err != nil {
			return err
		}
		files[normalized] = &gradient.ResultFile{Path: normalized, Size: uint64(info.Size()), Sha256: digest, LogFile: logFile}
		return nil
	}
	for _, logPath := range []string{"logs/setup.log", "logs/training.log"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(logPath))); err == nil {
			if err := add(logPath, true); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	for _, output := range config.Outputs {
		clean := filepath.ToSlash(filepath.Clean(output))
		if !safeRelative(clean) {
			collectionErrors = append(collectionErrors, fmt.Sprintf("unsafe configured output %q", output))
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(clean))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			missing = append(missing, clean)
			continue
		}
		if err != nil {
			collectionErrors = append(collectionErrors, err.Error())
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			collectionErrors = append(collectionErrors, fmt.Sprintf("configured output %q is a symlink", output))
			continue
		}
		if info.Mode().IsRegular() {
			if err := add(clean, false); err != nil {
				collectionErrors = append(collectionErrors, err.Error())
			}
			continue
		}
		if !info.IsDir() {
			collectionErrors = append(collectionErrors, fmt.Sprintf("configured output %q is unsupported", output))
			continue
		}
		err = filepath.WalkDir(path, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("output directory contains symlink %q", filePath)
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(root, filePath)
			if err != nil {
				return err
			}
			return add(filepath.ToSlash(relative), false)
		})
		if err != nil {
			collectionErrors = append(collectionErrors, err.Error())
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	manifest := make([]*gradient.ResultFile, 0, len(paths))
	for _, path := range paths {
		manifest = append(manifest, files[path])
	}
	return manifest, missing, collectionErrors, nil
}

func (m *Manager) Open(jobID, relativePath string) (*os.File, error) {
	manifest, _, _, err := m.Manifest(jobID)
	if err != nil {
		return nil, err
	}
	clean := filepath.ToSlash(filepath.Clean(relativePath))
	allowed := false
	for _, file := range manifest {
		if file.GetPath() == clean {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("result path is not declared")
	}
	root, err := m.workspace.Path(jobID)
	if err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(root, filepath.FromSlash(clean)))
}

func checksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func safeRelative(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func rejectSymlinkComponents(root, relative string) error {
	current := root
	for _, component := range strings.Split(filepath.ToSlash(filepath.Clean(relative)), "/") {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("result path contains symlink %q", relative)
		}
	}
	return nil
}
