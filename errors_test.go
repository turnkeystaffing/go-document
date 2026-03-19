package document

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderErrorErrorFormat(t *testing.T) {
	pe := &ProviderError{
		StatusCode:  400,
		Code:        "invalid_request",
		Description: "content is required",
	}
	assert.Equal(t, "document: invalid_request: content is required (HTTP 400)", pe.Error())
}

func TestProviderErrorErrorFormatWithoutStatusCode(t *testing.T) {
	pe := &ProviderError{
		Code:        "token_acquisition_failed",
		Description: "connection refused",
	}
	assert.Equal(t, "document: token_acquisition_failed: connection refused", pe.Error())
}

func TestProviderErrorErrorFormatEmptyFields(t *testing.T) {
	pe := &ProviderError{
		StatusCode: 400,
	}
	assert.Equal(t, "document: :  (HTTP 400)", pe.Error())
}

func TestProviderErrorErrorFormatEmptyCodeWithDescription(t *testing.T) {
	pe := &ProviderError{
		Description: "something went wrong",
	}
	assert.Equal(t, "document: : something went wrong", pe.Error())
}

func TestProviderErrorErrorsAs(t *testing.T) {
	pe := &ProviderError{
		StatusCode:  429,
		Code:        "rate_limited",
		Description: "too many requests",
		Retryable:   true,
	}
	var err error = pe

	var target *ProviderError
	require.True(t, errors.As(err, &target))
	assert.Equal(t, 429, target.StatusCode)
	assert.Equal(t, "rate_limited", target.Code)
	assert.True(t, target.Retryable)
}

func TestProviderErrorErrorsIsWithContextCanceled(t *testing.T) {
	pe := &ProviderError{
		StatusCode:  0,
		Code:        "request_failed",
		Description: "context canceled",
		Err:         context.Canceled,
	}
	assert.True(t, errors.Is(pe, context.Canceled))
}

func TestProviderErrorErrorsIsWithContextDeadlineExceeded(t *testing.T) {
	pe := &ProviderError{
		StatusCode:  0,
		Code:        "request_failed",
		Description: "deadline exceeded",
		Err:         context.DeadlineExceeded,
	}
	assert.True(t, errors.Is(pe, context.DeadlineExceeded))
}

func TestProviderErrorErrorsIsWithNilErr(t *testing.T) {
	pe := &ProviderError{
		StatusCode:  500,
		Code:        "server_error",
		Description: "internal error",
	}
	assert.False(t, errors.Is(pe, context.Canceled))
	assert.False(t, errors.Is(pe, context.DeadlineExceeded))
}

func TestProviderErrorUnwrap(t *testing.T) {
	underlying := errors.New("connection reset")
	pe := &ProviderError{
		Code:        "request_failed",
		Description: "connection reset",
		Err:         underlying,
	}
	assert.Equal(t, underlying, pe.Unwrap())
}

func TestProviderErrorUnwrapNil(t *testing.T) {
	pe := &ProviderError{
		Code:        "server_error",
		Description: "something went wrong",
	}
	assert.Nil(t, pe.Unwrap())
}

func TestIsRetryableRetryableStatuses(t *testing.T) {
	retryableStatuses := []int{408, 429, 503}
	for _, status := range retryableStatuses {
		pe := &ProviderError{
			StatusCode: status,
			Code:       "retryable",
			Retryable:  true,
		}
		assert.True(t, IsRetryable(pe), "status %d should be retryable", status)
	}
}

func TestIsRetryableNonRetryableStatuses(t *testing.T) {
	nonRetryable := []int{400, 401, 403, 500}
	for _, status := range nonRetryable {
		pe := &ProviderError{
			StatusCode: status,
			Code:       "non_retryable",
			Retryable:  false,
		}
		assert.False(t, IsRetryable(pe), "status %d should not be retryable", status)
	}
}

func TestIsRetryableNonProviderError(t *testing.T) {
	assert.False(t, IsRetryable(errors.New("generic error")))
}

func TestIsRetryableNilError(t *testing.T) {
	assert.False(t, IsRetryable(nil))
}

func TestProviderErrorWithRetryAfter(t *testing.T) {
	pe := &ProviderError{
		StatusCode:  429,
		Code:        "rate_limited",
		Description: "too many requests",
		Retryable:   true,
		RetryAfter:  30 * time.Second,
	}
	assert.Equal(t, 30*time.Second, pe.RetryAfter)
}

func TestProviderErrorWithoutRetryAfter(t *testing.T) {
	pe := &ProviderError{
		StatusCode:  500,
		Code:        "server_error",
		Description: "internal error",
		Retryable:   false,
	}
	assert.Equal(t, time.Duration(0), pe.RetryAfter)
}

func TestSentinelErrors(t *testing.T) {
	assert.EqualError(t, ErrResponseTooLarge, "document: response too large")
}

func TestErrResponseTooLargeWrapped(t *testing.T) {
	wrapped := fmt.Errorf("fetch failed: %w", ErrResponseTooLarge)
	assert.True(t, errors.Is(wrapped, ErrResponseTooLarge), "errors.Is should find wrapped ErrResponseTooLarge")
}

func TestProviderErrorErrorsAsWrapped(t *testing.T) {
	pe := &ProviderError{
		StatusCode:  429,
		Code:        "rate_limited",
		Description: "too many requests",
		Retryable:   true,
	}
	wrapped := fmt.Errorf("render failed: %w", pe)

	var target *ProviderError
	require.True(t, errors.As(wrapped, &target))
	assert.Equal(t, 429, target.StatusCode)
	assert.Equal(t, "rate_limited", target.Code)
	assert.True(t, target.Retryable)
}

func TestIsRetryableWrappedProviderError(t *testing.T) {
	pe := &ProviderError{
		StatusCode: 503,
		Code:       "service_unavailable",
		Retryable:  true,
	}
	wrapped := fmt.Errorf("operation failed: %w", pe)
	assert.True(t, IsRetryable(wrapped), "IsRetryable should find wrapped ProviderError")
}
