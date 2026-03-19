package document

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer creates an httptest.Server that returns the given status, headers, and body.
func newTestServer(t *testing.T, status int, headers map[string]string, body []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
}

// makeSuccessResponseBody builds a JSON response matching the gateway's RenderResponseDTO.
func makeSuccessResponseBody(t *testing.T, pdfData []byte, pages int, durationMs int64, blockedResources int) []byte {
	t.Helper()
	resp := map[string]interface{}{
		"data":         base64.StdEncoding.EncodeToString(pdfData),
		"content_type": ContentTypePDF,
		"metadata": map[string]interface{}{
			"pages":              pages,
			"render_duration_ms": durationMs,
		},
	}
	if blockedResources > 0 {
		resp["metadata"].(map[string]interface{})["blocked_resources"] = blockedResources
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	return b
}

// makeErrorResponseBody builds a JSON error response matching httputil.ErrorResponse.
func makeErrorResponseBody(t *testing.T, code, description string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{
		"error":             code,
		"error_description": description,
	})
	require.NoError(t, err)
	return b
}

func TestHTTPProviderSuccessfulRender(t *testing.T) {
	pdfData := []byte("%PDF-1.4 test content")
	body := makeSuccessResponseBody(t, pdfData, 3, 150, 0)

	srv := newTestServer(t, http.StatusOK, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	result, err := provider.Render(context.Background(), RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, pdfData, result.Data)
	assert.Equal(t, ContentTypePDF, result.ContentType)
	assert.Equal(t, "3", result.Metadata[MetadataKeyPages])
	assert.Equal(t, "150", result.Metadata[MetadataKeyRenderDurationMs])
	_, hasBlocked := result.Metadata[MetadataKeyBlockedResources]
	assert.False(t, hasBlocked, "blocked_resources should be absent when 0")
}

func TestHTTPProviderSuccessfulRenderWithBlockedResources(t *testing.T) {
	pdfData := []byte("%PDF-1.4")
	body := makeSuccessResponseBody(t, pdfData, 1, 200, 2)

	srv := newTestServer(t, http.StatusOK, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	result, err := provider.Render(context.Background(), RenderRequest{
		Content:     "<p>Test</p>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	assert.Equal(t, "2", result.Metadata[MetadataKeyBlockedResources])
}

func TestHTTPProviderMetadataConversion(t *testing.T) {
	pdfData := []byte("%PDF")
	body := makeSuccessResponseBody(t, pdfData, 42, 9999, 0)

	srv := newTestServer(t, http.StatusOK, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	result, err := provider.Render(context.Background(), RenderRequest{
		Content:     "test",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	assert.Equal(t, "42", result.Metadata[MetadataKeyPages], "int pages → string")
	assert.Equal(t, "9999", result.Metadata[MetadataKeyRenderDurationMs], "int64 duration → string")
}

func TestHTTPProviderRequestFormat(t *testing.T) {
	var receivedReq RenderRequest
	var receivedContentType, receivedAccept string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedAccept = r.Header.Get("Accept")
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	}))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, _ = provider.Render(context.Background(), RenderRequest{
		Content:     "<h1>Test</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	assert.Equal(t, "application/json", receivedContentType)
	assert.Equal(t, "application/json", receivedAccept)
	assert.Equal(t, "<h1>Test</h1>", receivedReq.Content)
	assert.Equal(t, ContentTypeHTML, receivedReq.ContentType)
	assert.Equal(t, FormatPDF, receivedReq.Format)
}

func TestHTTPProviderError400(t *testing.T) {
	body := makeErrorResponseBody(t, "invalid_request", "content is required")
	srv := newTestServer(t, http.StatusBadRequest, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	result, err := provider.Render(context.Background(), RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	assert.Nil(t, result)
	require.Error(t, err)

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 400, pe.StatusCode)
	assert.Equal(t, "invalid_request", pe.Code)
	assert.Equal(t, "content is required", pe.Description)
	assert.False(t, pe.Retryable)
}

func TestHTTPProviderError401(t *testing.T) {
	body := makeErrorResponseBody(t, "unauthorized", "invalid token")
	srv := newTestServer(t, http.StatusUnauthorized, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 401, pe.StatusCode)
	assert.Equal(t, "unauthorized", pe.Code)
	assert.False(t, pe.Retryable)
}

func TestHTTPProviderError403(t *testing.T) {
	body := makeErrorResponseBody(t, "forbidden", "insufficient scope")
	srv := newTestServer(t, http.StatusForbidden, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 403, pe.StatusCode)
	assert.False(t, pe.Retryable)
}

func TestHTTPProviderError408Retryable(t *testing.T) {
	body := makeErrorResponseBody(t, "render_timeout", "render exceeded timeout")
	srv := newTestServer(t, http.StatusRequestTimeout, map[string]string{"Retry-After": "5"}, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 408, pe.StatusCode)
	assert.True(t, pe.Retryable)
	assert.Equal(t, 5*time.Second, pe.RetryAfter)
}

func TestHTTPProviderError429WithRetryAfter(t *testing.T) {
	body := makeErrorResponseBody(t, "rate_limited", "too many requests")
	srv := newTestServer(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 429, pe.StatusCode)
	assert.Equal(t, "rate_limited", pe.Code)
	assert.True(t, pe.Retryable)
	assert.Equal(t, 30*time.Second, pe.RetryAfter)
	assert.True(t, IsRetryable(err))
}

func TestHTTPProviderError500(t *testing.T) {
	body := makeErrorResponseBody(t, "server_error", "internal error")
	srv := newTestServer(t, http.StatusInternalServerError, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 500, pe.StatusCode)
	assert.False(t, pe.Retryable)
}

func TestHTTPProviderError503WithRetryAfter(t *testing.T) {
	body := makeErrorResponseBody(t, "service_unavailable", "pool exhausted")
	srv := newTestServer(t, http.StatusServiceUnavailable, map[string]string{"Retry-After": "10"}, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 503, pe.StatusCode)
	assert.True(t, pe.Retryable)
	assert.Equal(t, 10*time.Second, pe.RetryAfter)
}

func TestHTTPProviderRetryableWithoutRetryAfterHeader(t *testing.T) {
	body := makeErrorResponseBody(t, "service_unavailable", "pool exhausted")
	srv := newTestServer(t, http.StatusServiceUnavailable, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 503, pe.StatusCode)
	assert.True(t, pe.Retryable, "503 should be retryable even without Retry-After header")
	assert.Equal(t, time.Duration(0), pe.RetryAfter, "RetryAfter should be zero when header is absent")
}

func TestHTTPProviderResponseBodySizeLimitExceeded(t *testing.T) {
	// Create a response larger than maxResponseSize.
	maxSize := int64(1024)
	largeBody := make([]byte, maxSize+100)
	for i := range largeBody {
		largeBody[i] = 'A'
	}

	srv := newTestServer(t, http.StatusOK, nil, largeBody)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{
		BaseURL:         srv.URL,
		MaxResponseSize: maxSize,
	})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.Error(t, err)
	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, "response_too_large", pe.Code)
	assert.True(t, errors.Is(err, ErrResponseTooLarge))
}

func TestHTTPProviderContextCancellation(t *testing.T) {
	// Server that delays long enough for cancellation to take effect.
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})

	done := make(chan error, 1)
	go func() {
		_, err := provider.Render(ctx, RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})
		done <- err
	}()

	<-started
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	case <-time.After(5 * time.Second):
		t.Fatal("test timed out waiting for cancellation")
	}
}

func TestHTTPProviderContextDeadlineExceeded(t *testing.T) {
	// Server that delays longer than the client deadline.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(ctx, RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestHTTPProviderHeaderFuncInjection(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	}))
	defer srv.Close()

	hf := func(_ context.Context) (http.Header, error) {
		h := http.Header{}
		h.Set("Authorization", "Bearer test-token-123")
		return h, nil
	}

	provider := NewHTTPProvider(HTTPProviderConfig{
		BaseURL:    srv.URL,
		HeaderFunc: hf,
	})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.NoError(t, err)
	assert.Equal(t, "Bearer test-token-123", receivedAuth)
}

func TestHTTPProviderHeaderFuncError(t *testing.T) {
	provider := NewHTTPProvider(HTTPProviderConfig{
		BaseURL: "http://localhost",
		HeaderFunc: func(_ context.Context) (http.Header, error) {
			return nil, fmt.Errorf("token expired")
		},
	})

	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.Error(t, err)
	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, "header_func_failed", pe.Code)
	assert.Equal(t, "header function returned an error", pe.Description, "description should be generic, not leak error details")
	assert.True(t, errors.Is(err, pe.Err), "original error should be accessible via Unwrap()")
	assert.Contains(t, pe.Err.Error(), "token expired", "original error preserved via Err field")
}

func TestHTTPProviderNilHeaderFunc(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	}))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.NoError(t, err)
	assert.Empty(t, receivedAuth, "no Authorization header when HeaderFunc is nil")
}

func TestHTTPProviderBearerTokenFromContext(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	}))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	ctx := withBearerToken(context.Background(), "context-token-456")
	_, err := provider.Render(ctx, RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.NoError(t, err)
	assert.Equal(t, "Bearer context-token-456", receivedAuth)
}

func TestHTTPProviderHeaderFuncPrecedenceOverContextToken(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	}))
	defer srv.Close()

	hf := func(_ context.Context) (http.Header, error) {
		h := http.Header{}
		h.Set("Authorization", "Bearer headerfunc-token")
		return h, nil
	}

	provider := NewHTTPProvider(HTTPProviderConfig{
		BaseURL:    srv.URL,
		HeaderFunc: hf,
	})
	ctx := withBearerToken(context.Background(), "context-token-should-be-ignored")
	_, err := provider.Render(ctx, RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.NoError(t, err)
	assert.Equal(t, "Bearer headerfunc-token", receivedAuth, "HeaderFunc should take precedence over context token")
}

func TestHTTPProviderConstructorPanicsOnEmptyBaseURL(t *testing.T) {
	assert.PanicsWithValue(t, "NewHTTPProvider: BaseURL is required", func() {
		NewHTTPProvider(HTTPProviderConfig{})
	})
}

func TestHTTPProviderConstructorStripsTrailingSlash(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, nil, makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL + "/"})
	assert.Equal(t, srv.URL, provider.baseURL)
}

func TestHTTPProviderConstructorDefaultHTTPClient(t *testing.T) {
	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: "http://example.com"})
	require.NotNil(t, provider.httpClient)
	assert.Equal(t, defaultHTTPTimeout, provider.httpClient.Timeout)
}

func TestHTTPProviderConstructorDefaultMaxResponseSize(t *testing.T) {
	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: "http://example.com"})
	assert.Equal(t, int64(DefaultMaxResponseSize), provider.maxResponseSize)
}

func TestHTTPProviderConstructorCustomMaxResponseSize(t *testing.T) {
	provider := NewHTTPProvider(HTTPProviderConfig{
		BaseURL:         "http://example.com",
		MaxResponseSize: 1024,
	})
	assert.Equal(t, int64(1024), provider.maxResponseSize)
}

func TestHTTPProviderConstructorNegativeMaxResponseSizeDefaulted(t *testing.T) {
	provider := NewHTTPProvider(HTTPProviderConfig{
		BaseURL:         "http://example.com",
		MaxResponseSize: -100,
	})
	assert.Equal(t, int64(DefaultMaxResponseSize), provider.maxResponseSize, "negative MaxResponseSize should default to DefaultMaxResponseSize")
}

func TestHTTPProviderMalformedJSONErrorResponse(t *testing.T) {
	srv := newTestServer(t, http.StatusBadGateway, nil, []byte("Bad Gateway from proxy"))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 502, pe.StatusCode)
	assert.Equal(t, "Bad Gateway", pe.Code, "should fallback to HTTP status text")
	assert.Equal(t, "Bad Gateway from proxy", pe.Description)
}

func TestHTTPProviderMalformedJSONErrorResponseTruncated(t *testing.T) {
	longBody := strings.Repeat("x", 500)
	srv := newTestServer(t, http.StatusBadGateway, nil, []byte(longBody))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Len(t, pe.Description, 256, "description should be truncated to 256 bytes")
}

func TestHTTPProviderMalformedJSONSuccessResponse(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, nil, []byte("not json"))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, "invalid_response", pe.Code)
	assert.Contains(t, pe.Description, "failed to parse response JSON")
}

func TestHTTPProviderMalformedBase64SuccessResponse(t *testing.T) {
	body := []byte(`{"data":"not-valid-base64!!!","content_type":"application/pdf","metadata":{"pages":1,"render_duration_ms":10}}`)
	srv := newTestServer(t, http.StatusOK, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, "invalid_response", pe.Code)
	assert.Contains(t, pe.Description, "failed to decode base64 data")
}

func TestHTTPProviderNegativeRetryAfterIgnored(t *testing.T) {
	body := makeErrorResponseBody(t, "rate_limited", "too many requests")
	srv := newTestServer(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "-1"}, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 429, pe.StatusCode)
	assert.True(t, pe.Retryable)
	assert.Equal(t, time.Duration(0), pe.RetryAfter, "negative Retry-After should result in zero duration")
}

func TestHTTPProviderLargeRetryAfterClamped(t *testing.T) {
	body := makeErrorResponseBody(t, "rate_limited", "too many requests")
	srv := newTestServer(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "9999999"}, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 429, pe.StatusCode)
	assert.True(t, pe.Retryable)
	assert.Equal(t, 3600*time.Second, pe.RetryAfter, "large Retry-After should be clamped to maxRetryAfterSeconds")
}

func TestHTTPProviderNonIntegerRetryAfterIgnored(t *testing.T) {
	body := makeErrorResponseBody(t, "rate_limited", "too many requests")
	srv := newTestServer(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "Thu, 01 Dec 2025 16:00:00 GMT"}, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 429, pe.StatusCode)
	assert.True(t, pe.Retryable)
	assert.Equal(t, time.Duration(0), pe.RetryAfter, "non-integer Retry-After should result in zero duration")
}

func TestHTTPProviderRequestMethodAndPath(t *testing.T) {
	var receivedMethod, receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	}))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.NoError(t, err)
	assert.Equal(t, "POST", receivedMethod)
	assert.Equal(t, "/v1/render", receivedPath)
}

func TestHTTPProviderEmptyBearerTokenInContextNotSent(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	}))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	ctx := withBearerToken(context.Background(), "")
	_, err := provider.Render(ctx, RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.NoError(t, err)
	assert.Empty(t, receivedAuth, "empty token in context should not set Authorization header")
}

func TestHTTPProviderCompileTimeAssertion(t *testing.T) {
	// Verify compile-time assertion exists (this test simply confirms the var declaration compiles).
	var p Provider = NewHTTPProvider(HTTPProviderConfig{BaseURL: "http://example.com"})
	assert.NotNil(t, p)
}

func TestHTTPProviderHeaderFuncMultiValueHeaders(t *testing.T) {
	var receivedValues []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedValues = r.Header.Values("X-Custom")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	}))
	defer srv.Close()

	hf := func(_ context.Context) (http.Header, error) {
		h := http.Header{}
		h.Add("X-Custom", "value1")
		h.Add("X-Custom", "value2")
		h.Add("X-Custom", "value3")
		return h, nil
	}

	provider := NewHTTPProvider(HTTPProviderConfig{
		BaseURL:    srv.URL,
		HeaderFunc: hf,
	})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.NoError(t, err)
	assert.Equal(t, []string{"value1", "value2", "value3"}, receivedValues, "all multi-value headers should be preserved")
}

func TestHTTPProviderConcurrentRenders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 5, 0))
	}))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})

	const goroutines = 10
	errs := make([]error, goroutines)
	results := make([]*RenderResult, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = provider.Render(context.Background(), RenderRequest{
				Content:     fmt.Sprintf("<p>Test %d</p>", idx),
				ContentType: ContentTypeHTML,
				Format:      FormatPDF,
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		assert.NoError(t, errs[i], "goroutine %d should succeed", i)
		assert.NotNil(t, results[i], "goroutine %d should return result", i)
	}
}

func TestHTTPProviderClientErrorNonContextual(t *testing.T) {
	// Create and immediately close a server to get a connection-refused error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.Error(t, err)
	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, "request_failed", pe.Code)
	assert.NotNil(t, pe.Err, "should wrap the underlying transport error")
	assert.False(t, errors.Is(err, context.Canceled), "should not be a context cancellation")
	assert.False(t, errors.Is(err, context.DeadlineExceeded), "should not be a deadline exceeded")
}

func TestHTTPProviderEmptyErrorFieldFallback(t *testing.T) {
	// Valid JSON with empty "error" field but populated error_description should preserve the description.
	body := []byte(`{"error":"","error_description":"something went wrong"}`)
	srv := newTestServer(t, http.StatusInternalServerError, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 500, pe.StatusCode)
	assert.Equal(t, "Internal Server Error", pe.Code, "should fall back to HTTP status text when error field is empty")
	assert.Equal(t, "something went wrong", pe.Description, "should extract error_description even when error field is empty")
}

func TestHTTPProviderRenderRequestMarshalFailure(t *testing.T) {
	nan := math.NaN()
	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: "http://localhost"})
	_, err := provider.Render(context.Background(), RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
		Options:     &RenderOptions{Scale: &nan},
	})

	require.Error(t, err)
	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, "invalid_request", pe.Code)
	assert.Contains(t, pe.Description, "failed to marshal request")
}

func TestHTTPProviderErrorBodyUTF8Truncation(t *testing.T) {
	// Build a body with multi-byte UTF-8 characters that will be split at the 256-byte boundary.
	// U+00E9 (é) is 2 bytes (0xC3 0xA9). Fill to force a split mid-character at byte 256.
	// 127 × "é" = 254 bytes, then add one more "é" (2 bytes) → 256 bytes, plus extra to exceed.
	multiByteBody := strings.Repeat("é", 127) + "éé" // 127*2 + 2*2 = 258 bytes
	require.Greater(t, len(multiByteBody), 256, "test body must exceed 256 bytes")

	srv := newTestServer(t, http.StatusBadGateway, nil, []byte(multiByteBody))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.True(t, len(pe.Description) <= 256, "description should be at most 256 bytes")
	assert.True(t, len(pe.Description) >= 254, "description should be at least 254 bytes (truncated at rune boundary)")
	// The truncated string must be valid UTF-8.
	assert.True(t, utf8.ValidString(pe.Description), "truncated description must be valid UTF-8")
}

func TestHTTPProviderEmptyResponseBody200(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, nil, []byte{})
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	require.Error(t, err)
	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, "invalid_response", pe.Code)
	assert.Contains(t, pe.Description, "failed to parse response JSON")
}

func TestHTTPProviderRetryAfterZeroIgnored(t *testing.T) {
	body := makeErrorResponseBody(t, "rate_limited", "too many requests")
	srv := newTestServer(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "0"}, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 429, pe.StatusCode)
	assert.True(t, pe.Retryable)
	assert.Equal(t, time.Duration(0), pe.RetryAfter, "Retry-After: 0 should result in zero duration")
}

func TestHTTPProviderRetryAfterIgnoredOnNonRetryableStatus(t *testing.T) {
	body := makeErrorResponseBody(t, "server_error", "internal error")
	srv := newTestServer(t, http.StatusInternalServerError, map[string]string{"Retry-After": "10"}, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 500, pe.StatusCode)
	assert.False(t, pe.Retryable, "500 should not be retryable")
	assert.Equal(t, time.Duration(0), pe.RetryAfter, "Retry-After should be ignored on non-retryable status")
}

func TestHTTPProviderBothErrorFieldsEmpty(t *testing.T) {
	body := []byte(`{"error":"","error_description":""}`)
	srv := newTestServer(t, http.StatusInternalServerError, nil, body)
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 500, pe.StatusCode)
	assert.Equal(t, "Internal Server Error", pe.Code, "should fallback to HTTP status text when both fields empty")
	assert.Equal(t, `{"error":"","error_description":""}`, pe.Description, "should use raw JSON body as description")
}

func TestHTTPProviderUnknownStatusCodeWithNonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(999)
		_, _ = w.Write([]byte("unknown error"))
	}))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{Content: "x", ContentType: ContentTypeHTML, Format: FormatPDF})

	var pe *ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 999, pe.StatusCode)
	assert.Equal(t, "", pe.Code, "unknown status code should produce empty Code")
	assert.Equal(t, "unknown error", pe.Description)
}

func TestHTTPProviderValidationBeforeHTTPCall(t *testing.T) {
	var requestReceived atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived.Store(true)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF"), 1, 10, 0))
	}))
	defer srv.Close()

	provider := NewHTTPProvider(HTTPProviderConfig{BaseURL: srv.URL})
	_, err := provider.Render(context.Background(), RenderRequest{})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve), "error should be *ValidationError")
	assert.Equal(t, "content", ve.Field)
	assert.Equal(t, "required", ve.Code)
	assert.False(t, requestReceived.Load(), "server should not receive request for invalid input")
}

func TestHTTPProviderConstructorMaxResponseSizeOverflowProtection(t *testing.T) {
	provider := NewHTTPProvider(HTTPProviderConfig{
		BaseURL:         "http://example.com",
		MaxResponseSize: math.MaxInt64,
	})
	assert.Equal(t, int64(math.MaxInt64-1), provider.maxResponseSize, "MaxInt64 should be clamped to MaxInt64-1 to prevent overflow")
}
