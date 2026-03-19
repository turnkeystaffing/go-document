package document

import "context"

// NoopProvider implements Provider with no side effects beyond input validation.
// It validates the request, then returns a stub RenderResult immediately without
// logging or I/O. Intended for tests and disabled-mode configurations.
//
// Stateless; safe for concurrent use.
type NoopProvider struct{}

// NewNoopProvider creates a NoopProvider. No configuration is needed.
func NewNoopProvider() *NoopProvider {
	return &NoopProvider{}
}

// Render returns a stub RenderResult. If the context is already cancelled,
// it returns the context error immediately.
func (p *NoopProvider) Render(ctx context.Context, req RenderRequest) (*RenderResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateRenderRequest(req); err != nil {
		return nil, err
	}
	return &RenderResult{ContentType: ContentTypePDF}, nil
}

// Compile-time assertion: NoopProvider implements Provider.
var _ Provider = (*NoopProvider)(nil)
