package sharder

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Assignment struct {
	WorkerID string
	Start    int
	End      int
	Count    int
	Path     string
}

func ShardJSONL(
	sourcePath string,
	relativeDatasetPath string,
	outputRoot string,
	workerIDs []string,
) ([]Assignment, error) {
	if len(workerIDs) == 0 {
		return nil, fmt.Errorf("at least one worker is required")
	}

	samples, err := readValidJSONLLines(sourcePath)
	if err != nil {
		return nil, err
	}
	if len(samples) < len(workerIDs) {
		return nil, fmt.Errorf(
			"dataset has %d samples but %d workers were selected",
			len(samples),
			len(workerIDs),
		)
	}

	assignments := computeAssignments(len(samples), workerIDs)
	cursor := 0
	for index := range assignments {
		assignment := &assignments[index]
		workerPath := filepath.Join(
			outputRoot,
			assignment.WorkerID,
			relativeDatasetPath,
		)
		if err := os.MkdirAll(filepath.Dir(workerPath), 0o755); err != nil {
			return nil, fmt.Errorf("create shard directory: %w", err)
		}
		end := cursor + assignment.Count
		if err := writeLines(workerPath, samples[cursor:end]); err != nil {
			return nil, fmt.Errorf("write shard for %s: %w", assignment.WorkerID, err)
		}
		assignment.Path = workerPath
		cursor = end
	}

	return assignments, nil
}

func computeAssignments(totalSamples int, workerIDs []string) []Assignment {
	base := totalSamples / len(workerIDs)
	remainder := totalSamples % len(workerIDs)
	assignments := make([]Assignment, 0, len(workerIDs))
	start := 1

	for index, workerID := range workerIDs {
		count := base
		if index < remainder {
			count++
		}
		end := start + count - 1
		assignments = append(assignments, Assignment{
			WorkerID: workerID,
			Start:    start,
			End:      end,
			Count:    count,
		})
		start = end + 1
	}
	return assignments
}

func readValidJSONLLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open JSONL dataset: %w", err)
	}
	defer file.Close()

	var samples []string
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if line == "" {
			continue
		}
		var sample map[string]any
		if err := json.Unmarshal([]byte(line), &sample); err != nil {
			return nil, fmt.Errorf("line %d is not valid JSON: %w", lineNumber, err)
		}
		samples = append(samples, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("dataset is empty")
	}
	return samples, nil
}

func writeLines(path string, lines []string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range lines {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			return err
		}
	}
	return writer.Flush()
}
