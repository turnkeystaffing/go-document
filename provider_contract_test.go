package document

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cross-file test dependencies:
//   - makeSuccessResponseBody (from http_provider_test.go) — used by contractTestHandler
//   - newFakeTokenServer (from authenticated_provider_test.go) — used by TestAuthenticatedProviderContract

// providerContract defines a contract test suite that all Provider implementations must pass.
type providerContract struct {
	// newProvider creates a fresh provider instance for each subtest.
	newProvider func(t *testing.T) Provider
}

// run executes all contract tests as subtests of the calling test function.
func (c providerContract) run(t *testing.T) {
	t.Helper()

	t.Run("Render/ValidRequest", func(t *testing.T) {
		provider := c.newProvider(t)
		result, err := provider.Render(context.Background(), RenderRequest{
			Content:     "<h1>test</h1>",
			ContentType: ContentTypeHTML,
			Format:      FormatPDF,
		})

		require.NoError(t, err)
		require.NotNil(t, result, "Render must return a non-nil result")
		assert.Equal(t, ContentTypePDF, result.ContentType)
		// Data and Metadata are not part of the behavioral contract — they vary
		// by provider (HTTPProvider returns PDF bytes; Noop/Log return nil).
	})

	t.Run("Render/ContextCancellation", func(t *testing.T) {
		provider := c.newProvider(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := provider.Render(ctx, RenderRequest{
			Content:     "<h1>test</h1>",
			ContentType: ContentTypeHTML,
			Format:      FormatPDF,
		})

		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled), "error must wrap context.Canceled")
	})

	t.Run("Render/ContextDeadlineExceeded", func(t *testing.T) {
		provider := c.newProvider(t)
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()
		<-ctx.Done()

		_, err := provider.Render(ctx, RenderRequest{
			Content:     "<h1>test</h1>",
			ContentType: ContentTypeHTML,
			Format:      FormatPDF,
		})

		require.Error(t, err)
		assert.True(t, errors.Is(err, context.DeadlineExceeded), "error must wrap context.DeadlineExceeded")
	})

	t.Run("Render/InvalidInput", func(t *testing.T) {
		provider := c.newProvider(t)
		_, err := provider.Render(context.Background(), RenderRequest{})

		require.Error(t, err)
		var ve *ValidationError
		require.True(t, errors.As(err, &ve), "error must unwrap to *ValidationError")
		assert.Equal(t, "content", ve.Field, "zero-value request must fail on content")
		assert.Equal(t, "required", ve.Code, "empty content must produce 'required' code")
	})

	t.Run("Render/ConcurrentSafety", func(t *testing.T) {
		provider := c.newProvider(t)
		const goroutines = 10
		errs := make([]error, goroutines)
		results := make([]*RenderResult, goroutines)
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				results[idx], errs[idx] = provider.Render(context.Background(), RenderRequest{
					Content:     "<h1>test</h1>",
					ContentType: ContentTypeHTML,
					Format:      FormatPDF,
				})
			}(i)
		}
		wg.Wait()

		for i := 0; i < goroutines; i++ {
			assert.NoError(t, errs[i], "goroutine %d should succeed", i)
			require.NotNil(t, results[i], "goroutine %d should return result", i)
		}
	})
}

// contractTestHandler returns an http.Handler that validates POST method, /v1/render path,
// and Content-Type: application/json header. Used as the shared base for contract test servers.
func contractTestHandler(t *testing.T, requireAuth bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain request body for proper HTTP connection lifecycle.
		defer func() { _, _ = io.Copy(io.Discard, r.Body) }()

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v1/render" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}
		if requireAuth {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// pages=1, render_duration_ms=42, blocked_resources=0 (stub values for contract test)
		_, _ = w.Write(makeSuccessResponseBody(t, []byte("%PDF-stub"), 1, 42, 0))
	})
}

// newContractTestServer creates an httptest server returning a valid render response.
// Validates HTTP method (POST), path (/v1/render), and Content-Type header.
func newContractTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(contractTestHandler(t, false))
	t.Cleanup(server.Close)
	return server
}

// newAuthContractTestServer creates an httptest server that requires a valid
// Bearer token in the Authorization header. Returns 401 if missing or malformed.
func newAuthContractTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(contractTestHandler(t, true))
	t.Cleanup(server.Close)
	return server
}

func TestNoopProviderContract(t *testing.T) {
	providerContract{
		newProvider: func(t *testing.T) Provider {
			return NewNoopProvider()
		},
	}.run(t)
}

func TestLogProviderContract(t *testing.T) {
	providerContract{
		newProvider: func(t *testing.T) Provider {
			return NewLogProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
		},
	}.run(t)
}

func TestHTTPProviderContract(t *testing.T) {
	providerContract{
		newProvider: func(t *testing.T) Provider {
			server := newContractTestServer(t)
			return NewHTTPProvider(HTTPProviderConfig{BaseURL: server.URL})
		},
	}.run(t)
}

func TestAuthenticatedProviderContract(t *testing.T) {
	providerContract{
		newProvider: func(t *testing.T) Provider {
			ts := newFakeTokenServer(t) // from authenticated_provider_test.go (same package)
			t.Cleanup(ts.Close)
			ds := newAuthContractTestServer(t)

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			provider, err := NewAuthenticatedProvider(AuthenticatedProviderConfig{
				BaseURL:      ds.URL,
				ClientID:     "contract-test-client",
				ClientSecret: "contract-test-secret",
				TokenURL:     ts.URL + "/oauth/token",
			}, logger)
			require.NoError(t, err)
			t.Cleanup(func() { provider.Close() })

			return provider
		},
	}.run(t)
}
