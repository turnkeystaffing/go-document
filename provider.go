// Package document provides the Go SDK for document rendering.
//
// Consumers import this package to render documents (HTML/Markdown to PDF)
// via a Provider interface. This package defines the public API types and
// interface. It does not import any internal packages.
//
// Provider implementations included (or planned) in this package:
//
//   - HTTPProvider: calls the document rendering service over HTTP
//   - AuthenticatedProvider: wraps HTTPProvider with OAuth2 token injection
//   - LogProvider: renders to structured log output (development/debugging)
//   - NoopProvider: returns zero-value results (testing/disabled mode)
package document

import "context"

// Provider renders documents. Implementations translate RenderRequest into
// the appropriate backend call and return the result.
//
// All implementations in this package validate the request via
// [ValidateRenderRequest] before performing any I/O, token acquisition, or
// logging. Invalid requests return [*ValidationError] without side effects.
//
// Security contract for implementations:
//   - Production providers must use TLS for all network communication.
//   - The provided context.Context must be propagated for tracing, cancellation,
//     and deadline support. Never create context.Background() inside a provider.
//   - RenderRequest.Content and RenderResult.Data may contain sensitive data
//     (PII, financial, legal). Implementations must not log these fields.
//   - HTTP-based providers should limit response body size to prevent OOM from
//     oversized server responses (use io.LimitReader or http.MaxBytesReader).
type Provider interface {
	Render(ctx context.Context, req RenderRequest) (*RenderResult, error)
}
