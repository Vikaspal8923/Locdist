package project

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultSpecFile = "ldgcc.yaml"

type Spec struct {
	Job        JobSpec
	Entrypoint string
	Dataset    DatasetSpec
	Workers    WorkerSpec
	Outputs    []string
}

type JobSpec struct {
	Name string
}

type DatasetSpec struct {
	Train string
}

type WorkerSpec struct {
	Count int
}

func LoadSpec(projectRoot string) (Spec, error) {
	specPath := filepath.Join(projectRoot, DefaultSpecFile)
	if _, err := os.Stat(filepath.Join(projectRoot, "ldgcc.yml")); err == nil {
		specPath = filepath.Join(projectRoot, "ldgcc.yml")
	}
	file, err := os.Open(specPath)
	if err != nil {
		return Spec{}, fmt.Errorf("open project spec: %w", err)
	}
	defer file.Close()

	spec, err := parse(file)
	if err != nil {
		return Spec{}, fmt.Errorf("parse %s: %w", specPath, err)
	}
	if err := spec.Validate(projectRoot); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func (s Spec) Validate(projectRoot string) error {
	if strings.TrimSpace(s.Entrypoint) == "" {
		return fmt.Errorf("entrypoint is required")
	}
	if strings.TrimSpace(s.Dataset.Train) == "" {
		return fmt.Errorf("dataset.train is required")
	}
	if s.Workers.Count <= 0 {
		return fmt.Errorf("workers.count must be greater than zero")
	}
	if filepath.IsAbs(s.Entrypoint) || filepath.IsAbs(s.Dataset.Train) {
		return fmt.Errorf("entrypoint and dataset paths must be relative")
	}
	seenOutputs := make(map[string]struct{}, len(s.Outputs))
	for _, output := range s.Outputs {
		if strings.TrimSpace(output) == "" || filepath.IsAbs(output) || !withinProject(projectRoot, output) {
			return fmt.Errorf("output paths must be relative and stay inside project")
		}
		clean := filepath.ToSlash(filepath.Clean(output))
		if _, exists := seenOutputs[clean]; exists {
			return fmt.Errorf("duplicate output path %q", output)
		}
		seenOutputs[clean] = struct{}{}
	}
	if !withinProject(projectRoot, s.Entrypoint) ||
		!withinProject(projectRoot, s.Dataset.Train) {
		return fmt.Errorf("entrypoint and dataset paths must stay inside project")
	}
	if _, err := os.Stat(filepath.Join(projectRoot, s.Entrypoint)); err != nil {
		return fmt.Errorf("entrypoint is not readable: %w", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, s.Dataset.Train)); err != nil {
		return fmt.Errorf("dataset.train is not readable: %w", err)
	}
	return nil
}

func parse(file *os.File) (Spec, error) {
	var spec Spec
	section := ""
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := stripComment(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := leadingSpaces(line)
		trimmed := strings.TrimSpace(line)
		if section == "outputs" && strings.HasPrefix(trimmed, "- ") {
			value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")), `"'`)
			if value == "" {
				return Spec{}, fmt.Errorf("output path cannot be empty")
			}
			spec.Outputs = append(spec.Outputs, value)
			continue
		}
		key, value, ok := splitKeyValue(strings.TrimSpace(line))
		if !ok {
			return Spec{}, fmt.Errorf("invalid line %q", scanner.Text())
		}

		if indent == 0 && value == "" {
			section = key
			continue
		}
		if indent == 0 {
			section = ""
		}

		switch {
		case section == "job" && key == "name":
			spec.Job.Name = value
		case section == "dataset" && key == "train":
			spec.Dataset.Train = value
		case section == "workers" && key == "count":
			count, err := strconv.Atoi(value)
			if err != nil {
				return Spec{}, fmt.Errorf("workers.count must be a number")
			}
			spec.Workers.Count = count
		case section == "outputs" && key == "outputs":
			return Spec{}, fmt.Errorf("outputs must be a YAML list")
		case section == "" && key == "entrypoint":
			spec.Entrypoint = value
		default:
			return Spec{}, fmt.Errorf("unsupported field %q", key)
		}
	}
	if err := scanner.Err(); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func stripComment(line string) string {
	before, _, _ := strings.Cut(line, "#")
	return before
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func splitKeyValue(line string) (string, string, bool) {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	return key, value, key != ""
}

func withinProject(projectRoot string, relativePath string) bool {
	cleanRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return false
	}
	cleanPath, err := filepath.Abs(filepath.Join(projectRoot, relativePath))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}
