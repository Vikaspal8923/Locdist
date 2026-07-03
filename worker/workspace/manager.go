package workspace

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxPackageBytes   = 10 << 30
	MaxRPCBytes       = 128 << 20
	MaxExtractedBytes = 20 << 30
)

type Manager struct{ root string }

func New(root string) *Manager { return &Manager{root: root} }

func (m *Manager) Root() string { return m.root }

func (m *Manager) Path(jobID string) (string, error) {
	if !safeID(jobID) {
		return "", fmt.Errorf("job_id is unsafe")
	}
	path := filepath.Join(m.root, jobID)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workspace for job %q does not exist", jobID)
	}
	return path, nil
}

func (m *Manager) Remove(jobID string) error {
	if !safeID(jobID) {
		return fmt.Errorf("job_id is unsafe")
	}
	return os.RemoveAll(filepath.Join(m.root, jobID))
}

func (m *Manager) Prepare(jobID, entrypoint, datasetPath string, archive []byte) (string, error) {
	if len(archive) == 0 || len(archive) > MaxPackageBytes {
		return "", fmt.Errorf("workspace package size is invalid")
	}
	return m.prepare(jobID, entrypoint, datasetPath, bytes.NewReader(archive), int64(len(archive)))
}

func (m *Manager) PrepareFile(jobID, entrypoint, datasetPath, archivePath string) (string, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return "", err
	}
	if info.Size() <= 0 || info.Size() > MaxPackageBytes {
		return "", fmt.Errorf("workspace package size is invalid")
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	return m.prepare(jobID, entrypoint, datasetPath, archive, info.Size())
}

func (m *Manager) prepare(jobID, entrypoint, datasetPath string, archive io.ReaderAt, archiveSize int64) (string, error) {
	if !safeID(jobID) || !safeRelative(entrypoint) || !safeRelative(datasetPath) {
		return "", fmt.Errorf("workspace metadata contains an unsafe path")
	}
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Name() == jobID {
			continue
		}
		if err := os.RemoveAll(filepath.Join(m.root, entry.Name())); err != nil {
			return "", fmt.Errorf("clear previous workspace: %w", err)
		}
	}
	destination := filepath.Join(m.root, jobID)
	if _, err := os.Stat(destination); err == nil {
		return "", fmt.Errorf("workspace for job %q already exists", jobID)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	temporary, err := os.MkdirTemp(m.root, ".prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	if err := extract(archive, archiveSize, temporary); err != nil {
		return "", err
	}
	for _, required := range []string{entrypoint, "job_config.json"} {
		info, err := os.Stat(filepath.Join(temporary, filepath.FromSlash(required)))
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("required workspace file %q is missing", required)
		}
	}
	if info, err := os.Stat(filepath.Join(temporary, filepath.FromSlash(datasetPath))); err != nil || !(info.Mode().IsRegular() || info.IsDir()) {
		return "", fmt.Errorf("required workspace dataset %q is missing", datasetPath)
	}
	for _, directory := range []string{"logs", "artifacts"} {
		if err := os.MkdirAll(filepath.Join(temporary, directory), 0o700); err != nil {
			return "", err
		}
	}
	if err := os.Rename(temporary, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func extract(archive io.ReaderAt, size int64, destination string) error {
	reader, err := zip.NewReader(archive, size)
	if err != nil {
		return fmt.Errorf("open workspace package: %w", err)
	}
	var total uint64
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		if !safeRelative(name) || file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe archive entry %q", file.Name)
		}
		total += file.UncompressedSize64
		if total > MaxExtractedBytes {
			return fmt.Errorf("workspace exceeds extracted size limit")
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(output, source)
		closeErr := output.Close()
		source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func safeID(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.Contains(value, "\\")
}

func safeRelative(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}
