package errors

import "errors"

var (
	ErrInvalidRuntimeVersion = errors.New(
		"runtime_version must be greater than zero",
	)

	ErrMissingJobID = errors.New(
		"job_id is required",
	)

	ErrMissingWorkerID = errors.New(
		"worker_id is required",
	)

	ErrMissingChunks = errors.New(
		"at least one gradient chunk is required",
	)
)
