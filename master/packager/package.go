package packager

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Vikaspal8923/Locdist/master/project"
)

const (
	MaxPackageBytes = 10 << 30
	MaxRPCBytes     = MaxPackageBytes + (1 << 20)
)

type PackageRequest struct {
	ProjectRoot   string
	JobID         string
	WorkerID      string
	Entrypoint    string
	DatasetPath   string
	ShardPath     string
	ShardKind     string
	Outputs       []string
	Communication project.CommunicationSpec
}

type JobConfig struct {
	JobID         string                    `json:"job_id"`
	WorkerID      string                    `json:"worker_id"`
	Entrypoint    string                    `json:"entrypoint"`
	DatasetPath   string                    `json:"dataset_path"`
	Outputs       []string                  `json:"outputs,omitempty"`
	Communication project.CommunicationSpec `json:"communication,omitempty"`
}

func Build(request PackageRequest) ([]byte, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	if err := Write(&buffer, request); err != nil {
		return nil, err
	}
	if buffer.Len() > MaxPackageBytes {
		return nil, fmt.Errorf("workspace package exceeds %d bytes", MaxPackageBytes)
	}
	return buffer.Bytes(), nil
}

func Write(target io.Writer, request PackageRequest) error {
	if err := validateRequest(request); err != nil {
		return err
	}
	writer := zip.NewWriter(target)
	if err := addProjectFiles(writer, request); err != nil {
		_ = writer.Close()
		return err
	}
	if err := addJobConfig(writer, request); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close workspace package: %w", err)
	}
	return nil
}

func validateRequest(request PackageRequest) error {
	if request.ProjectRoot == "" ||
		request.JobID == "" ||
		request.WorkerID == "" ||
		request.Entrypoint == "" ||
		request.DatasetPath == "" ||
		request.ShardPath == "" {
		return fmt.Errorf("workspace package request is incomplete")
	}
	return nil
}

func addProjectFiles(writer *zip.Writer, request PackageRequest) error {
	return filepath.WalkDir(
		request.ProjectRoot,
		func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relativePath, err := filepath.Rel(request.ProjectRoot, path)
			if err != nil {
				return err
			}
			if relativePath == "." {
				return nil
			}
			relativePath = filepath.ToSlash(relativePath)
			if shouldSkip(relativePath, entry) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if request.ShardKind == "directory" && relativePath == filepath.ToSlash(request.DatasetPath) {
					if err := addDirectory(writer, request.ShardPath, relativePath); err != nil {
						return err
					}
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if request.ShardKind != "directory" && relativePath == filepath.ToSlash(request.DatasetPath) {
				return addFile(writer, request.ShardPath, relativePath)
			}
			return addFile(writer, path, relativePath)
		},
	)
}

func addDirectory(writer *zip.Writer, sourceRoot string, archiveRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		return addFile(writer, path, filepath.ToSlash(filepath.Join(archiveRoot, relative)))
	})
}

func shouldSkip(relativePath string, entry os.DirEntry) bool {
	parts := strings.Split(relativePath, "/")
	for _, part := range parts {
		switch part {
		case ".git", ".venv", "venv", "__pycache__", ".pytest_cache", ".ldgcc", "ldgcc_jobs":
			return true
		}
	}
	return strings.HasSuffix(entry.Name(), ".pyc")
}

func addFile(writer *zip.Writer, sourcePath string, archivePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", sourcePath, err)
	}
	defer source.Close()

	header := &zip.FileHeader{
		Name:   archivePath,
		Method: zip.Deflate,
	}
	header.SetMode(0o644)
	target, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create archive entry %s: %w", archivePath, err)
	}
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("write archive entry %s: %w", archivePath, err)
	}
	return nil
}

func addJobConfig(writer *zip.Writer, request PackageRequest) error {
	data, err := json.MarshalIndent(
		JobConfig{
			JobID:         request.JobID,
			WorkerID:      request.WorkerID,
			Entrypoint:    request.Entrypoint,
			DatasetPath:   request.DatasetPath,
			Outputs:       request.Outputs,
			Communication: request.Communication,
		},
		"",
		"  ",
	)
	if err != nil {
		return err
	}

	header := &zip.FileHeader{
		Name:   "job_config.json",
		Method: zip.Deflate,
	}
	header.SetMode(0o644)
	target, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = target.Write(append(data, '\n'))
	return err
}
