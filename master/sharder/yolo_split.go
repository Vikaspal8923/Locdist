package sharder

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type yoloSample struct {
	imageSource string
	imageRel    string
	labelSource string
	labelRel    string
}

func ShardYOLOSplit(
	sourcePath string,
	relativeDatasetPath string,
	outputRoot string,
	workerIDs []string,
) ([]Assignment, error) {
	if len(workerIDs) == 0 {
		return nil, fmt.Errorf("at least one worker is required")
	}

	samples, err := listYOLOSamples(sourcePath)
	if err != nil {
		return nil, err
	}
	if len(samples) < len(workerIDs) {
		return nil, fmt.Errorf("dataset has %d samples but %d workers were selected", len(samples), len(workerIDs))
	}

	assignments := computeAssignments(len(samples), workerIDs)
	cursor := 0
	for index := range assignments {
		assignment := &assignments[index]
		workerRoot := filepath.Join(outputRoot, assignment.WorkerID, relativeDatasetPath)
		end := cursor + assignment.Count
		for _, sample := range samples[cursor:end] {
			if err := copyFile(sample.imageSource, filepath.Join(workerRoot, filepath.FromSlash(sample.imageRel))); err != nil {
				return nil, fmt.Errorf("copy YOLO image shard for %s: %w", assignment.WorkerID, err)
			}
			labelTarget := filepath.Join(workerRoot, filepath.FromSlash(sample.labelRel))
			if sample.labelSource == "" {
				if err := writeEmptyFile(labelTarget); err != nil {
					return nil, fmt.Errorf("create empty YOLO label shard for %s: %w", assignment.WorkerID, err)
				}
				continue
			}
			if err := copyFile(sample.labelSource, labelTarget); err != nil {
				return nil, fmt.Errorf("copy YOLO label shard for %s: %w", assignment.WorkerID, err)
			}
		}
		assignment.Path = workerRoot
		assignment.Kind = "directory"
		cursor = end
	}

	return assignments, nil
}

func listYOLOSamples(sourcePath string) ([]yoloSample, error) {
	imagesRoot := filepath.Join(sourcePath, "images")
	labelsRoot := filepath.Join(sourcePath, "labels")

	if info, err := os.Stat(imagesRoot); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("images is not a directory")
		}
		return nil, fmt.Errorf("yolo_split dataset requires %s: %w", filepath.ToSlash(filepath.Join(sourcePath, "images")), err)
	}
	if info, err := os.Stat(labelsRoot); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("labels is not a directory")
		}
		return nil, fmt.Errorf("yolo_split dataset requires %s: %w", filepath.ToSlash(filepath.Join(sourcePath, "labels")), err)
	}

	samples := []yoloSample{}
	err := filepath.WalkDir(imagesRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !isImageFile(entry.Name()) {
			return nil
		}
		relative, err := filepath.Rel(imagesRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		labelRelative := strings.TrimSuffix(relative, filepath.Ext(relative)) + ".txt"
		labelSource := filepath.Join(labelsRoot, filepath.FromSlash(labelRelative))
		if info, err := os.Stat(labelSource); err != nil || info.IsDir() {
			labelSource = ""
		}
		samples = append(samples, yoloSample{
			imageSource: path,
			imageRel:    filepath.ToSlash(filepath.Join("images", relative)),
			labelSource: labelSource,
			labelRel:    filepath.ToSlash(filepath.Join("labels", labelRelative)),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read yolo_split dataset: %w", err)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("yolo_split dataset has no image files")
	}
	sort.Slice(samples, func(left, right int) bool {
		return samples[left].imageRel < samples[right].imageRel
	})
	return samples, nil
}

func writeEmptyFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, nil, 0o644)
}
