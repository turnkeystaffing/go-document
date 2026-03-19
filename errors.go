package document

import (
	"errors"
	"fmt"
	"time"
)

// ProviderError represents an error returned by a Provider implementation.
// Consumers can type-assert with errors.As() or check retryability via IsRetryable().
// The Err field enables errors.Is() chain traversal (e.g., errors.Is(err, context.Canceled)).
type ProviderError struct {
	// StatusCode is the HTTP status code (0 when not applicable).
	StatusCode int
	// Code is a machine-readable error code (e.g., "invalid_request", "rate_limited").
	Code string
	// Description is a human-readable error description.
	Description string
	// Retryable indicates whether the operation may succeed on retry.
	Retryable bool
	// RetryAfter is the suggested wait duration before retrying (parsed from Retry-After header).
	RetryAfter time.Duration
	// Err is the optional underlying cause (e.g., context.Canceled, context.DeadlineExceeded).
	Err error
}

// Error implements the error interface.
func (e *ProviderError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("document: %s: %s (HTTP %d)", e.Code, e.Description, e.StatusCode)
	}
	return fmt.Sprintf("document: %s: %s", e.Code, e.Description)
}

// Unwrap returns the underlying error for errors.Is() chain traversal.
func (e *ProviderError) Unwrap() error {
	return e.Err
}

// IsRetryable checks if err is a retryable *ProviderError.
func IsRetryable(err error) bool {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Retryable
	}
	return false
}

// Sentinel errors for common provider failure modes.
var (
	// ErrResponseTooLarge indicates the server response exceeded the configured maximum size.
	ErrResponseTooLarge = errors.New("document: response too large")
)
