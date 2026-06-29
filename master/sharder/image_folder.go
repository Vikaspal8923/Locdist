package sharder

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var imageExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".bmp":  {},
	".gif":  {},
	".webp": {},
}

type imageSample struct {
	className string
	source    string
	relative  string
}

func ShardImageFolder(
	sourcePath string,
	relativeDatasetPath string,
	outputRoot string,
	workerIDs []string,
) ([]Assignment, error) {
	if len(workerIDs) == 0 {
		return nil, fmt.Errorf("at least one worker is required")
	}
	samplesByClass, totalSamples, err := listImageFolderSamples(sourcePath)
	if err != nil {
		return nil, err
	}
	if totalSamples < len(workerIDs) {
		return nil, fmt.Errorf("dataset has %d image samples but %d workers were selected", totalSamples, len(workerIDs))
	}

	assignments := make([]Assignment, 0, len(workerIDs))
	counts := make(map[string]int, len(workerIDs))
	starts := make(map[string]int, len(workerIDs))
	for index, workerID := range workerIDs {
		assignments = append(assignments, Assignment{WorkerID: workerID, Kind: "directory"})
		starts[workerID] = index + 1
	}

	for _, className := range sortedClassNames(samplesByClass) {
		samples := samplesByClass[className]
		for sampleIndex, sample := range samples {
			workerID := workerIDs[sampleIndex%len(workerIDs)]
			counts[workerID]++
			targetRoot := filepath.Join(outputRoot, workerID, relativeDatasetPath)
			targetPath := filepath.Join(targetRoot, filepath.FromSlash(sample.relative))
			if err := copyFile(sample.source, targetPath); err != nil {
				return nil, fmt.Errorf("copy image shard for %s: %w", workerID, err)
			}
		}
	}

	for index := range assignments {
		assignment := &assignments[index]
		assignment.Count = counts[assignment.WorkerID]
		assignment.Start = starts[assignment.WorkerID]
		assignment.End = assignment.Start + assignment.Count - 1
		assignment.Path = filepath.Join(outputRoot, assignment.WorkerID, relativeDatasetPath)
	}
	return assignments, nil
}

func listImageFolderSamples(sourcePath string) (map[string][]imageSample, int, error) {
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return nil, 0, fmt.Errorf("read image folder dataset: %w", err)
	}
	samplesByClass := make(map[string][]imageSample)
	total := 0
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		className := entry.Name()
		classRoot := filepath.Join(sourcePath, className)
		err := filepath.WalkDir(classRoot, func(path string, item os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if item.IsDir() {
				return nil
			}
			if item.Type()&os.ModeSymlink != 0 || !isImageFile(item.Name()) {
				return nil
			}
			relative, err := filepath.Rel(sourcePath, path)
			if err != nil {
				return err
			}
			samplesByClass[className] = append(samplesByClass[className], imageSample{className: className, source: path, relative: filepath.ToSlash(relative)})
			total++
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	if len(samplesByClass) == 0 || total == 0 {
		return nil, 0, fmt.Errorf("image_folder dataset has no class image files")
	}
	for className := range samplesByClass {
		sort.Slice(samplesByClass[className], func(left, right int) bool {
			return samplesByClass[className][left].relative < samplesByClass[className][right].relative
		})
	}
	return samplesByClass, total, nil
}

func sortedClassNames(samples map[string][]imageSample) []string {
	classes := make([]string, 0, len(samples))
	for className := range samples {
		classes = append(classes, className)
	}
	sort.Strings(classes)
	return classes
}

func isImageFile(name string) bool {
	_, ok := imageExtensions[strings.ToLower(filepath.Ext(name))]
	return ok
}

func copyFile(sourcePath string, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer target.Close()
	_, err = io.Copy(target, source)
	return err
}
