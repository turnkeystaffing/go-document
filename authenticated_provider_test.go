package document

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFakeTokenServer creates a test HTTP server that returns OAuth tokens at /oauth/token.
func newFakeTokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "fake-token-abc",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
}

func TestNewAuthenticatedProvider_Success(t *testing.T) {
	t.Parallel()

	tokenServer := newFakeTokenServer(t)
	defer tokenServer.Close()

	provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
		Scopes:       "document:render",
	}, slog.Default())
	require.NoError(t, err)
	defer provider.Close()

	assert.NotNil(t, provider)
}

func TestNewAuthenticatedProvider_MissingBaseURL(t *testing.T) {
	t.Parallel()

	_, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		ClientID:     "test",
		ClientSecret: "secret",
		TokenURL:     "https://auth.example.com/oauth/token",
	}, slog.Default())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "base URL is required")
}

func TestNewAuthenticatedProvider_MissingClientID(t *testing.T) {
	t.Parallel()

	_, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientSecret: "secret",
		TokenURL:     "https://auth.example.com/oauth/token",
	}, slog.Default())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "client ID is required")
}

func TestNewAuthenticatedProvider_MissingClientSecret(t *testing.T) {
	t.Parallel()

	_, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:  "https://docs.example.com",
		ClientID: "test",
		TokenURL: "https://auth.example.com/oauth/token",
	}, slog.Default())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "client secret is required")
}

func TestNewAuthenticatedProvider_MissingTokenURL(t *testing.T) {
	t.Parallel()

	_, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientID:     "test",
		ClientSecret: "secret",
	}, slog.Default())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "token URL is required")
}

func TestNewAuthenticatedProvider_NilLoggerPanics(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "document.NewAuthenticatedProvider: logger cannot be nil", func() {
		_, _ = NewAuthenticatedProvider(AuthenticatedProviderConfig{
			BaseURL:      "https://docs.example.com",
			ClientID:     "test",
			ClientSecret: "secret",
			TokenURL:     "https://auth.example.com/oauth/token",
		}, nil)
	})
}

func TestNewAuthenticatedProvider_DefaultHTTPTimeout(t *testing.T) {
	t.Parallel()

	tokenServer := newFakeTokenServer(t)
	defer tokenServer.Close()

	// Zero timeout should default to 10s
	provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientID:     "test",
		ClientSecret: "secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
		HTTPTimeout:  0,
	}, slog.Default())
	require.NoError(t, err)
	defer provider.Close()
	assert.NotNil(t, provider)

	// Negative timeout should also default to 10s
	provider2, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientID:     "test",
		ClientSecret: "secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
		HTTPTimeout:  -5 * time.Second,
	}, slog.Default())
	require.NoError(t, err)
	defer provider2.Close()
	assert.NotNil(t, provider2)
}

func TestAuthenticatedProvider_SuccessfulRender(t *testing.T) {
	t.Parallel()

	tokenServer := newFakeTokenServer(t)
	defer tokenServer.Close()

	pdfData := []byte("%PDF-1.4 authenticated content")
	var receivedAuth string
	docServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, pdfData, 2, 100, 0))
	}))
	defer docServer.Close()

	provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      docServer.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
		Scopes:       "document:render",
		HTTPTimeout:  5 * time.Second,
	}, slog.Default())
	require.NoError(t, err)
	defer provider.Close()

	result, err := provider.Render(context.Background(), RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, pdfData, result.Data)
	assert.Equal(t, ContentTypePDF, result.ContentType)
	assert.Equal(t, "2", result.Metadata[MetadataKeyPages])
	assert.Equal(t, "Bearer fake-token-abc", receivedAuth, "token should be injected via context into Authorization header")
}

func TestAuthenticatedProvider_ValidationBeforeTokenAcquisition(t *testing.T) {
	t.Parallel()

	var tokenRequested atomic.Bool
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequested.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "should-not-be-fetched",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
	}, slog.Default())
	require.NoError(t, err)
	defer provider.Close()

	// Empty request should fail validation BEFORE token acquisition
	_, err = provider.Render(context.Background(), RenderRequest{})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve), "error should be *ValidationError")
	assert.Equal(t, "content", ve.Field)
	assert.Equal(t, "required", ve.Code)
	assert.False(t, tokenRequested.Load(), "token server should NOT be contacted for invalid requests")
}

func TestAuthenticatedProvider_TokenAcquisitionFailure(t *testing.T) {
	t.Parallel()

	// Token server that always returns errors
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server_error"}`))
	}))
	defer tokenServer.Close()

	provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
		HTTPTimeout:  2 * time.Second,
	}, slog.Default())
	require.NoError(t, err)
	defer provider.Close()

	_, err = provider.Render(context.Background(), RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.Error(t, err)
	var pe *ProviderError
	require.True(t, errors.As(err, &pe), "error should be *ProviderError")
	assert.Equal(t, "token_acquisition_failed", pe.Code)
	assert.Equal(t, "failed to acquire access token", pe.Description)
	assert.NotNil(t, pe.Err, "should wrap the underlying token error")
}

func TestAuthenticatedProvider_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Slow token server — will be cancelled before responding
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer tokenServer.Close()

	provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
		HTTPTimeout:  10 * time.Second,
	}, slog.Default())
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = provider.Render(ctx, RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.Error(t, err)
	var pe *ProviderError
	require.True(t, errors.As(err, &pe), "error should be *ProviderError")
	assert.Equal(t, "token_acquisition_failed", pe.Code)
}

func TestAuthenticatedProvider_ConcurrentRenders(t *testing.T) {
	t.Parallel()

	tokenServer := newFakeTokenServer(t)
	defer tokenServer.Close()

	pdfData := []byte("%PDF-concurrent")
	docServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeSuccessResponseBody(t, pdfData, 1, 10, 0))
	}))
	defer docServer.Close()

	provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      docServer.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
		HTTPTimeout:  5 * time.Second,
	}, slog.Default())
	require.NoError(t, err)
	defer provider.Close()

	const goroutines = 10
	errs := make([]error, goroutines)
	results := make([]*RenderResult, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = provider.Render(context.Background(), RenderRequest{
				Content:     "<p>Test</p>",
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

func TestAuthenticatedProvider_CloseIdempotent(t *testing.T) {
	t.Parallel()

	tokenServer := newFakeTokenServer(t)
	defer tokenServer.Close()

	provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientID:     "test",
		ClientSecret: "secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
	}, slog.Default())
	require.NoError(t, err)

	require.NoError(t, provider.Close())
	require.NoError(t, provider.Close()) // idempotent — second call must also succeed
}

func TestAuthenticatedProvider_CompileTimeAssertion(t *testing.T) {
	t.Parallel()

	tokenServer := newFakeTokenServer(t)
	defer tokenServer.Close()

	ap, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      "https://docs.example.com",
		ClientID:     "test",
		ClientSecret: "secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
	}, slog.Default())
	require.NoError(t, err)
	defer ap.Close()

	// Verify AuthenticatedProvider satisfies Provider interface
	var p Provider = ap
	assert.NotNil(t, p)
}

func TestAuthenticatedProvider_ServerErrorPropagation(t *testing.T) {
	t.Parallel()

	tokenServer := newFakeTokenServer(t)
	defer tokenServer.Close()

	docServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(makeErrorResponseBody(t, "server_error", "internal error"))
	}))
	defer docServer.Close()

	provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
		BaseURL:      docServer.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL + "/oauth/token",
		HTTPTimeout:  5 * time.Second,
	}, slog.Default())
	require.NoError(t, err)
	defer provider.Close()

	_, err = provider.Render(context.Background(), RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.Error(t, err)
	var pe *ProviderError
	require.True(t, errors.As(err, &pe), "error should be *ProviderError from document server")
	assert.Equal(t, 500, pe.StatusCode)
	assert.Equal(t, "server_error", pe.Code)
	assert.Equal(t, "internal error", pe.Description)
}
