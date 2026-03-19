package document

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/turnkeystaffing/go-authclient"
)

// defaultAuthHTTPTimeout is the default timeout for the authenticated provider's
// token acquisition HTTP requests.
const defaultAuthHTTPTimeout = 10 * time.Second

// AuthenticatedProviderConfig configures an OAuth-authenticated document provider.
// The provider uses client_credentials grant to obtain tokens from the auth
// service, caches them in memory with proactive refresh at 80% lifetime,
// and injects Bearer tokens into all outbound HTTP requests via context.
type AuthenticatedProviderConfig struct {
	// BaseURL is the document service base URL (required, e.g. "https://docs.internal").
	BaseURL string
	// ClientID is the OAuth client ID (required).
	ClientID string
	// ClientSecret is the OAuth client secret (required).
	ClientSecret string
	// TokenURL is the auth server token endpoint (required, e.g. "https://auth.internal/oauth/token").
	TokenURL string
	// Scopes is space-separated OAuth scopes to request (optional, e.g. "document:render").
	Scopes string
	// HTTPTimeout is the timeout for token acquisition HTTP requests (default: 10s).
	HTTPTimeout time.Duration
}

func (c *AuthenticatedProviderConfig) validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("document: authenticated provider: base URL is required")
	}
	if c.ClientID == "" {
		return fmt.Errorf("document: authenticated provider: client ID is required")
	}
	if c.ClientSecret == "" {
		return fmt.Errorf("document: authenticated provider: client secret is required")
	}
	if c.TokenURL == "" {
		return fmt.Errorf("document: authenticated provider: token URL is required")
	}
	if c.HTTPTimeout <= 0 {
		c.HTTPTimeout = defaultAuthHTTPTimeout
	}
	return nil
}

// AuthenticatedProvider wraps an HTTPProvider with OAuth token management.
// It implements Provider for rendering documents and io.Closer for graceful shutdown.
type AuthenticatedProvider struct {
	provider      Provider
	tokenProvider *authclient.OAuthTokenProvider
}

// NewAuthenticatedProvider creates a document provider authenticated via OAuth
// client_credentials grant. The returned provider:
//   - Obtains and caches tokens using go-authclient's OAuthTokenProvider
//   - Proactively refreshes tokens at 80% of their lifetime
//   - Injects Bearer tokens into context for HTTPProvider to read
//
// Call Close() on shutdown to release the token provider resources.
func NewAuthenticatedProvider(cfg AuthenticatedProviderConfig, logger *slog.Logger) (*AuthenticatedProvider, error) {
	if logger == nil {
		panic("document.NewAuthenticatedProvider: logger cannot be nil")
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	tokenProvider, err := authclient.NewOAuthTokenProvider(authclient.OAuthTokenProviderConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     cfg.TokenURL,
		Scopes:       cfg.Scopes,
		HTTPTimeout:  cfg.HTTPTimeout,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("document: authenticated provider: token provider: %w", err)
	}

	httpProvider := NewHTTPProvider(HTTPProviderConfig{
		BaseURL: cfg.BaseURL,
	})

	return &AuthenticatedProvider{
		provider:      httpProvider,
		tokenProvider: tokenProvider,
	}, nil
}

// Render validates the request, acquires an OAuth token, and delegates to the
// internal HTTPProvider. Validation happens before token acquisition to avoid
// wasted OAuth token fetches for invalid requests.
func (ap *AuthenticatedProvider) Render(ctx context.Context, req RenderRequest) (*RenderResult, error) {
	if err := ValidateRenderRequest(req); err != nil {
		return nil, err
	}

	token, err := ap.tokenProvider.Token(ctx)
	if err != nil {
		return nil, &ProviderError{
			Code:        "token_acquisition_failed",
			Description: "failed to acquire access token",
			Err:         err,
		}
	}

	if token == "" {
		return nil, &ProviderError{
			Code:        "empty_token",
			Description: "token provider returned an empty token",
		}
	}

	enrichedCtx := withBearerToken(ctx, token)
	return ap.provider.Render(enrichedCtx, req)
}

// Close shuts down the token provider, cancelling any in-flight refresh
// goroutines and clearing cached tokens. Idempotent.
func (ap *AuthenticatedProvider) Close() error {
	return ap.tokenProvider.Close()
}

// Compile-time assertion: AuthenticatedProvider implements Provider.
var _ Provider = (*AuthenticatedProvider)(nil)
