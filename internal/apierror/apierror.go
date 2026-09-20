// Package apierror defines structured CTXT-XXXX error codes for CLI and HTTP responses.
package apierror

import "fmt"

// Error code constants — CTXT-XXXX namespace.
const (
	// Ingestion (1XXX)
	CodeIngestInvalidInput   = "CTXT-1001"
	CodeIngestUnsupportedFmt = "CTXT-1002"
	CodeIngestFileMissing    = "CTXT-1003"
	CodeIngestEnqueueFailed  = "CTXT-1004"
	CodeIngestNothingFound   = "CTXT-1005"
	CodeIngestTokenMissing   = "CTXT-1006"

	// Search (2XXX)
	CodeSearchQueryRequired  = "CTXT-2001"
	CodeSearchInvalidFilter  = "CTXT-2002"
	CodeSearchInternalFailed = "CTXT-2003"

	// Config (3XXX)
	CodeConfigPathMissing = "CTXT-3001"
	CodeConfigWriteFailed = "CTXT-3002"
	CodeConfigInvalid     = "CTXT-3003"

	// Registry (4XXX)
	CodeRegistryNotFound      = "CTXT-4001"
	CodeRegistryURLRequired   = "CTXT-4002"
	CodeRegistryFetchFailed   = "CTXT-4003"
	CodeRegistryBundleMissing = "CTXT-4004"
	CodeRegistryInvalidBundle = "CTXT-4005"

	// Pipeline (5XXX)
	CodePipelineNameRequired  = "CTXT-5001"
	CodePipelineStepsRequired = "CTXT-5002"
	CodePipelineNotFound      = "CTXT-5003"
	CodePipelineRunFailed     = "CTXT-5004"
	CodePipelineInvalidBody   = "CTXT-5005"

	// Storage (6XXX)
	CodeStoragePathMissing = "CTXT-6001"
	CodeStorageInitFailed  = "CTXT-6002"
	CodeStorageReadFailed  = "CTXT-6003"
	CodeStorageWriteFailed = "CTXT-6004"
	CodeStorageNotFound    = "CTXT-6005"

	// Generic
	CodeNotFound      = "CTXT-9001"
	CodeInvalidInput  = "CTXT-9002"
	CodeInternalError = "CTXT-9003"
	CodeUnhealthy     = "CTXT-9004"
)

// APIError is a structured user-facing error with a CTXT-XXXX code.
type APIError struct {
	Code    string
	Message string
	Err     error
}

func (e *APIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *APIError) Unwrap() error { return e.Err }

// JSONBody returns the JSON-serialisable shape: {"error":{"code":"...","message":"..."}}.
func (e *APIError) JSONBody() map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":    e.Code,
			"message": e.Message,
		},
	}
}

// New constructs an APIError.
func New(code, message string, err error) *APIError {
	return &APIError{Code: code, Message: message, Err: err}
}

// Named constructors.

// ErrNotFound signals a requested resource was not found.
func ErrNotFound(resource string) *APIError {
	return New(CodeNotFound, resource+" not found", nil)
}

// ErrInvalidInput signals bad user input.
func ErrInvalidInput(detail string) *APIError {
	return New(CodeInvalidInput, detail, nil)
}

// ErrPipelineFailed signals a pipeline execution error.
func ErrPipelineFailed(err error) *APIError {
	return New(CodePipelineRunFailed, "pipeline execution failed", err)
}

// ErrStorageInit signals storage initialisation failure.
func ErrStorageInit(err error) *APIError {
	return New(CodeStorageInitFailed, "storage initialisation failed", err)
}

// ErrRegistryNotFound signals an unknown registry name.
func ErrRegistryNotFound(name string) *APIError {
	return New(CodeRegistryNotFound, "registry "+name+" not found in config", nil)
}
