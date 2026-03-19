package document

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultMaxResponseSize is the default maximum response body size (55MB).
// This accounts for the server's 50MB PDF cap plus base64 encoding overhead
// and JSON wrapper.
const DefaultMaxResponseSize = 55 << 20 // 55MB

// defaultHTTPTimeout is the default timeout for the HTTP client.
const defaultHTTPTimeout = 60 * time.Second

// maxRetryAfterSeconds caps the Retry-After header parsing to prevent
// excessively large values from causing indefinite waits in consumer retry loops.
const maxRetryAfterSeconds = 3600

// maxErrorDescriptionBytes is the maximum number of bytes included in the
// Description field of a ProviderError when the response body cannot be parsed
// as structured JSON. Truncation to this limit happens on the raw byte slice
// before string conversion, avoiding a large intermediate string allocation.
// A subsequent UTF-8 validity pass on the (small) resulting string trims any
// incomplete multi-byte sequence at the truncation boundary.
const maxErrorDescriptionBytes = 256

// renderEndpoint is the path for the document rendering API endpoint.
const renderEndpoint = "/v1/render"

// HeaderFunc returns dynamic HTTP headers to include with each request.
// Used by AuthenticatedProvider to inject Bearer tokens, or by consumers
// for custom header injection (API keys, correlation IDs, etc.).
// If HeaderFunc returns an error, the render call fails with that error.
type HeaderFunc func(ctx context.Context) (http.Header, error)

// HTTPProviderConfig configures an HTTPProvider.
type HTTPProviderConfig struct {
	// BaseURL is the document service base URL (e.g., "https://docs.example.com").
	// Required. Trailing slashes are stripped.
	BaseURL string
	// HTTPClient is the HTTP client used for requests. Defaults to a new
	// *http.Client with 60s timeout if nil.
	HTTPClient *http.Client
	// MaxResponseSize is the maximum response body size in bytes. Defaults to
	// DefaultMaxResponseSize (55MB) if zero.
	MaxResponseSize int64
	// HeaderFunc is called before each request to get dynamic headers.
	// If nil, no extra headers are added.
	HeaderFunc HeaderFunc
}

// HTTPProvider implements Provider by calling the document rendering service over HTTP.
// All fields are read-only after construction; safe for concurrent use.
type HTTPProvider struct {
	baseURL         string
	httpClient      *http.Client
	maxResponseSize int64
	headerFunc      HeaderFunc
}

// NewHTTPProvider creates an HTTPProvider with the given configuration.
// Panics if BaseURL is empty.
func NewHTTPProvider(config HTTPProviderConfig) *HTTPProvider {
	if config.BaseURL == "" {
		panic("NewHTTPProvider: BaseURL is required")
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")

	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	if config.MaxResponseSize <= 0 {
		config.MaxResponseSize = DefaultMaxResponseSize
	} else if config.MaxResponseSize > math.MaxInt64-1 {
		// Prevent integer overflow in io.LimitReader(resp.Body, maxResponseSize+1).
		config.MaxResponseSize = math.MaxInt64 - 1
	}

	return &HTTPProvider{
		baseURL:         config.BaseURL,
		httpClient:      config.HTTPClient,
		maxResponseSize: config.MaxResponseSize,
		headerFunc:      config.HeaderFunc,
	}
}

// bearerTokenKeyType is the package-private context key for passing bearer tokens
// from AuthenticatedProvider to HTTPProvider.
type bearerTokenKeyType struct{}

// withBearerToken stores a bearer token in the context for HTTPProvider to read.
func withBearerToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, bearerTokenKeyType{}, token)
}

// bearerTokenFromContext extracts the bearer token from context, if present.
func bearerTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(bearerTokenKeyType{}).(string)
	return token
}

// renderResponseDTO mirrors the gateway's JSON response format.
// Defined locally to avoid importing internal packages.
type renderResponseDTO struct {
	Data        string                 `json:"data"`
	ContentType string                 `json:"content_type"`
	Metadata    renderResponseMetadata `json:"metadata"`
}

type renderResponseMetadata struct {
	Pages            int   `json:"pages"`
	RenderDurationMs int64 `json:"render_duration_ms"`
	BlockedResources int   `json:"blocked_resources,omitempty"`
}

// errorResponseDTO mirrors the gateway's error response format.
type errorResponseDTO struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Render validates the request, then sends it to the document service over HTTP and returns the result.
func (p *HTTPProvider) Render(ctx context.Context, req RenderRequest) (*RenderResult, error) {
	if err := ValidateRenderRequest(req); err != nil {
		return nil, err
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, &ProviderError{
			Code:        "invalid_request",
			Description: fmt.Sprintf("failed to marshal request: %s", err),
			Err:         err,
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+renderEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &ProviderError{
			Code:        "request_failed",
			Description: fmt.Sprintf("failed to create request: %s", err),
			Err:         err,
		}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Apply dynamic headers from HeaderFunc (e.g., Authorization).
	if p.headerFunc != nil {
		headers, headerErr := p.headerFunc(ctx)
		if headerErr != nil {
			return nil, &ProviderError{
				Code:        "header_func_failed",
				Description: "header function returned an error",
				Err:         headerErr,
			}
		}
		for key, values := range headers {
			httpReq.Header.Del(key)
			for _, v := range values {
				httpReq.Header.Add(key, v)
			}
		}
	}

	// If no Authorization header set by HeaderFunc, check context for bearer token.
	if httpReq.Header.Get("Authorization") == "" {
		if token := bearerTokenFromContext(ctx); token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, p.wrapClientError(ctx, err)
	}
	defer resp.Body.Close()

	// Read body with size limit.
	limitedReader := io.LimitReader(resp.Body, p.maxResponseSize+1)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, &ProviderError{
			StatusCode:  resp.StatusCode,
			Code:        "read_failed",
			Description: fmt.Sprintf("failed to read response body: %s", err),
			Err:         err,
		}
	}

	// Check if response exceeded size limit.
	if int64(len(respBody)) > p.maxResponseSize {
		return nil, &ProviderError{
			StatusCode:  resp.StatusCode,
			Code:        "response_too_large",
			Description: fmt.Sprintf("response body exceeds maximum size of %d bytes", p.maxResponseSize),
			Err:         ErrResponseTooLarge,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseErrorResponse(resp, respBody)
	}

	return p.parseSuccessResponse(respBody)
}

// wrapClientError wraps HTTP client errors, detecting context cancellation/deadline.
func (p *HTTPProvider) wrapClientError(ctx context.Context, err error) *ProviderError {
	// Check context error first — it's more specific than the wrapped HTTP error.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return &ProviderError{
			Code:        "request_failed",
			Description: ctxErr.Error(),
			Err:         ctxErr,
		}
	}
	return &ProviderError{
		Code:        "request_failed",
		Description: err.Error(),
		Err:         err,
	}
}

// parseErrorResponse converts a non-200 HTTP response into a *ProviderError.
func (p *HTTPProvider) parseErrorResponse(resp *http.Response, body []byte) *ProviderError {
	pe := &ProviderError{
		StatusCode: resp.StatusCode,
		Retryable:  isRetryableStatus(resp.StatusCode),
	}

	// Parse Retry-After header for retryable status codes.
	// Negative values are rejected; values above maxRetryAfterSeconds are clamped.
	if pe.Retryable {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if seconds, parseErr := strconv.Atoi(ra); parseErr == nil && seconds > 0 {
				if seconds > maxRetryAfterSeconds {
					seconds = maxRetryAfterSeconds
				}
				pe.RetryAfter = time.Duration(seconds) * time.Second
			}
		}
	}

	// Try to parse structured error response.
	var errResp errorResponseDTO
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		pe.Code = errResp.Error
		pe.Description = errResp.ErrorDescription
	} else if err == nil && errResp.ErrorDescription != "" {
		// Valid JSON with empty error field but populated description.
		pe.Code = http.StatusText(resp.StatusCode)
		pe.Description = errResp.ErrorDescription
	} else {
		// Fallback: use HTTP status text and truncated body.
		pe.Code = http.StatusText(resp.StatusCode)
		truncated := body
		if len(truncated) > maxErrorDescriptionBytes {
			truncated = truncated[:maxErrorDescriptionBytes]
		}
		desc := string(truncated)
		// Ensure truncation doesn't split a multi-byte UTF-8 character.
		for !utf8.ValidString(desc) && len(desc) > 0 {
			desc = desc[:len(desc)-1]
		}
		pe.Description = desc
	}

	return pe
}

// parseSuccessResponse parses a 200 OK JSON response into a RenderResult.
func (p *HTTPProvider) parseSuccessResponse(body []byte) (*RenderResult, error) {
	var dto renderResponseDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		return nil, &ProviderError{
			StatusCode:  http.StatusOK,
			Code:        "invalid_response",
			Description: fmt.Sprintf("failed to parse response JSON: %s", err),
			Err:         err,
		}
	}

	data, err := base64.StdEncoding.DecodeString(dto.Data)
	if err != nil {
		return nil, &ProviderError{
			StatusCode:  http.StatusOK,
			Code:        "invalid_response",
			Description: fmt.Sprintf("failed to decode base64 data: %s", err),
			Err:         err,
		}
	}

	metadata := map[string]string{
		MetadataKeyPages:            strconv.Itoa(dto.Metadata.Pages),
		MetadataKeyRenderDurationMs: strconv.FormatInt(dto.Metadata.RenderDurationMs, 10),
	}
	if dto.Metadata.BlockedResources > 0 {
		metadata[MetadataKeyBlockedResources] = strconv.Itoa(dto.Metadata.BlockedResources)
	}

	return &RenderResult{
		Data:        data,
		ContentType: dto.ContentType,
		Metadata:    metadata,
	}, nil
}

// isRetryableStatus returns true for HTTP status codes that are retryable.
func isRetryableStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code == http.StatusServiceUnavailable
}

// Compile-time assertion: HTTPProvider implements Provider.
var _ Provider = (*HTTPProvider)(nil)
