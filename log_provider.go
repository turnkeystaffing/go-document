package document

import (
	"context"
	"log/slog"
)

// LogProvider implements Provider by logging render request metadata to a
// structured logger. It returns a stub RenderResult without performing actual
// rendering. Intended for local development and debugging.
//
// Security: LogProvider never logs RenderRequest.Content or CustomCSS values
// (may contain PII). Only metadata about the request is logged.
//
// The logger field is immutable after construction; safe for concurrent use.
type LogProvider struct {
	logger *slog.Logger
}

// NewLogProvider creates a LogProvider that logs to the given logger.
// If logger is nil, slog.Default() is used as the fallback (captured at
// construction time; subsequent calls to slog.SetDefault do not affect
// this provider).
func NewLogProvider(logger *slog.Logger) *LogProvider {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogProvider{logger: logger}
}

// Render validates the request, logs structured metadata, and returns a stub
// RenderResult. If the context is already cancelled, it returns the context
// error immediately without validation or logging.
//
// Context is checked once before logging. Since logging and result construction
// are non-blocking, mid-operation cancellation does not affect the result.
func (p *LogProvider) Render(ctx context.Context, req RenderRequest) (*RenderResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := ValidateRenderRequest(req); err != nil {
		return nil, err
	}

	p.logger.InfoContext(ctx, "document render request",
		slog.String("content_type", req.ContentType),
		slog.String("format", req.Format),
		slog.Int("content_size", len(req.Content)),
		slog.Bool("has_options", req.Options != nil),
		slog.Bool("has_custom_css", req.CustomCSS != ""),
	)

	return &RenderResult{ContentType: ContentTypePDF}, nil
}

// Compile-time assertion: LogProvider implements Provider.
var _ Provider = (*LogProvider)(nil)
