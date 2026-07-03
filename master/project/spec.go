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
	Job           JobSpec
	Entrypoint    string
	Dataset       DatasetSpec
	Workers       WorkerSpec
	Outputs       []string
	Communication CommunicationSpec
}

type JobSpec struct {
	Name string
}

type DatasetSpec struct {
	Train string
	Type  string
}

type WorkerSpec struct {
	Count int
}

type CommunicationSpec struct {
	Precision   string          `json:"precision,omitempty"`
	Compression CompressionSpec `json:"compression,omitempty"`
}

type CompressionSpec struct {
	Type             string  `json:"type,omitempty"`
	Mode             string  `json:"mode,omitempty"`
	TopK             string  `json:"top_k,omitempty"`
	Selection        string  `json:"selection,omitempty"`
	SampleRate       string  `json:"sample_rate,omitempty"`
	MaxPayloadFactor float64 `json:"max_payload_factor,omitempty"`
	Device           string  `json:"device,omitempty"`
	ErrorFeedback    bool    `json:"error_feedback"`
	WarmupSteps      int     `json:"warmup_steps,omitempty"`
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
	spec.NormalizePaths()
	if err := spec.Validate(projectRoot); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func (s *Spec) NormalizePaths() {
	s.Entrypoint = normalizeSpecPath(s.Entrypoint)
	s.Dataset.Train = normalizeSpecPath(s.Dataset.Train)
	for index, output := range s.Outputs {
		s.Outputs[index] = normalizeSpecPath(output)
	}
}

func (s Spec) Validate(projectRoot string) error {
	if strings.TrimSpace(s.Entrypoint) == "" {
		return fmt.Errorf("entrypoint is required")
	}
	if strings.TrimSpace(s.Dataset.Train) == "" {
		return fmt.Errorf("dataset.train is required")
	}
	datasetType := s.Dataset.Type
	if datasetType == "" {
		datasetType = "jsonl"
	}
	if datasetType != "jsonl" && datasetType != "image_folder" {
		return fmt.Errorf("dataset.type must be jsonl or image_folder")
	}
	if s.Workers.Count <= 0 {
		return fmt.Errorf("workers.count must be greater than zero")
	}
	if !isPortableRelativePath(s.Entrypoint) || !isPortableRelativePath(s.Dataset.Train) {
		return fmt.Errorf("entrypoint and dataset paths must be relative")
	}
	seenOutputs := make(map[string]struct{}, len(s.Outputs))
	for _, output := range s.Outputs {
		if !isPortableRelativePath(output) || !withinProject(projectRoot, output) {
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
	if err := s.Communication.Validate(); err != nil {
		return err
	}
	return nil
}

func (c CommunicationSpec) Validate() error {
	if c.Precision != "" && c.Precision != "fp32" && c.Precision != "fp16" {
		return fmt.Errorf("communication.precision must be fp32 or fp16")
	}
	if c.Compression.Type == "" {
		return nil
	}
	if c.Compression.Type != "none" && c.Compression.Type != "topk" {
		return fmt.Errorf("communication.compression.type must be none or topk")
	}
	if c.Compression.Type == "topk" {
		if c.Compression.Mode != "" && c.Compression.Mode != "global" && c.Compression.Mode != "per_layer" {
			return fmt.Errorf("communication.compression.mode must be global or per_layer")
		}
		if c.Compression.Selection != "" && c.Compression.Selection != "exact" && c.Compression.Selection != "sampled_threshold" {
			return fmt.Errorf("communication.compression.selection must be exact or sampled_threshold")
		}
		if c.Compression.TopK != "" {
			if err := validatePercent(c.Compression.TopK); err != nil {
				return err
			}
		}
		if c.Compression.SampleRate != "" {
			if err := validatePercentField(c.Compression.SampleRate, "communication.compression.sample_rate"); err != nil {
				return err
			}
		}
		if c.Compression.MaxPayloadFactor != 0 && c.Compression.MaxPayloadFactor < 1.0 {
			return fmt.Errorf("communication.compression.max_payload_factor must be >= 1.0")
		}
		if c.Compression.Device != "" && c.Compression.Device != "auto" && c.Compression.Device != "cpu" && c.Compression.Device != "gpu" {
			return fmt.Errorf("communication.compression.device must be auto, cpu, or gpu")
		}
		if !c.Compression.ErrorFeedback {
			return fmt.Errorf("communication.compression.error_feedback must be true for topk")
		}
		if c.Compression.WarmupSteps < 0 {
			return fmt.Errorf("communication.compression.warmup_steps must be non-negative")
		}
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
		if indent == 2 && section == "communication" && key == "compression" && value == "" {
			section = "communication.compression"
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
		case section == "dataset" && key == "type":
			spec.Dataset.Type = value
		case section == "workers" && key == "count":
			count, err := strconv.Atoi(value)
			if err != nil {
				return Spec{}, fmt.Errorf("workers.count must be a number")
			}
			spec.Workers.Count = count
		case section == "outputs" && key == "outputs":
			return Spec{}, fmt.Errorf("outputs must be a YAML list")
		case section == "communication" && key == "precision":
			spec.Communication.Precision = value
		case section == "communication" && key == "compression":
			spec.Communication.Compression.Type = value
			spec.Communication.Compression.ErrorFeedback = true
		case section == "communication.compression" && key == "type":
			spec.Communication.Compression.Type = value
			spec.Communication.Compression.ErrorFeedback = true
		case section == "communication.compression" && key == "mode":
			spec.Communication.Compression.Mode = value
		case section == "communication.compression" && key == "top_k":
			spec.Communication.Compression.TopK = value
		case section == "communication.compression" && key == "selection":
			spec.Communication.Compression.Selection = value
		case section == "communication.compression" && key == "sample_rate":
			spec.Communication.Compression.SampleRate = value
		case section == "communication.compression" && key == "max_payload_factor":
			factor, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return Spec{}, fmt.Errorf("communication.compression.max_payload_factor must be numeric")
			}
			spec.Communication.Compression.MaxPayloadFactor = factor
		case section == "communication.compression" && key == "device":
			spec.Communication.Compression.Device = value
		case section == "communication.compression" && key == "error_feedback":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return Spec{}, fmt.Errorf("communication.compression.error_feedback must be true or false")
			}
			spec.Communication.Compression.ErrorFeedback = parsed
		case section == "communication.compression" && key == "warmup_steps":
			steps, err := strconv.Atoi(value)
			if err != nil {
				return Spec{}, fmt.Errorf("communication.compression.warmup_steps must be a number")
			}
			spec.Communication.Compression.WarmupSteps = steps
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

func validatePercent(value string) error {
	return validatePercentField(value, "communication.compression.top_k")
}

func validatePercentField(value string, fieldName string) error {
	if !strings.HasSuffix(value, "%") {
		return fmt.Errorf("%s must be a percent string", fieldName)
	}
	number := strings.TrimSpace(strings.TrimSuffix(value, "%"))
	percent, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return fmt.Errorf("%s must be numeric", fieldName)
	}
	if percent <= 0 || percent > 100 {
		return fmt.Errorf("%s must be > 0%% and <= 100%%", fieldName)
	}
	return nil
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

func normalizeSpecPath(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
}

func isPortableRelativePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "\\") || hasWindowsVolume(value) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func hasWindowsVolume(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':'
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
