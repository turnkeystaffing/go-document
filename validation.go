package document

import "fmt"

// ValidationError represents a client-side input validation failure.
// It is returned by ValidateRenderRequest before any network call or I/O.
type ValidationError struct {
	Field   string // The request field that failed validation (e.g., "content", "content_type")
	Code    string // Machine-readable code: "required", "too_large", "unsupported"
	Message string // Human-readable description
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("document: validation: %s: %s", e.Field, e.Message)
}

// Validation constants matching server-side defaults.
const (
	MaxContentSize   = 10 * 1024 * 1024 // 10MB — matches server default
	MaxCustomCSSSize = 256 * 1024       // 256KB — matches server default
)

// ValidateRenderRequest validates a RenderRequest against client-side rules
// matching the server's validation. Returns nil if valid.
// Returns *ValidationError on the first validation failure (fail-fast).
//
// Note: Options fields (paper_size, orientation, scale, margins) are validated
// server-side only and are not checked here.
//
// Content type and format comparisons are case-sensitive, matching the server.
//
// Note: For empty format or content_type, this function returns code "required".
// The server returns "unsupported" for the same empty values because it uses a
// single check (e.g., format != "pdf"). SDK error codes may differ from server
// error codes for empty-field cases.
//
// Validation order matches server-side render_service.go:
//  1. Content required
//  2. Content size ≤ MaxContentSize
//  3. Format required
//  4. Format supported (pdf)
//  5. Content type required
//  6. Content type supported (text/html, text/markdown)
//  7. CustomCSS size ≤ MaxCustomCSSSize (markdown only)
func ValidateRenderRequest(req RenderRequest) error {
	if req.Content == "" {
		return &ValidationError{
			Field:   "content",
			Code:    "required",
			Message: "content is required",
		}
	}

	if len(req.Content) > MaxContentSize {
		return &ValidationError{
			Field:   "content",
			Code:    "too_large",
			Message: fmt.Sprintf("content size %d exceeds maximum %d bytes", len(req.Content), MaxContentSize),
		}
	}

	if req.Format == "" {
		return &ValidationError{
			Field:   "format",
			Code:    "required",
			Message: "format is required",
		}
	}

	if req.Format != FormatPDF {
		return &ValidationError{
			Field:   "format",
			Code:    "unsupported",
			Message: fmt.Sprintf("unsupported format: supported formats are [%s]", FormatPDF),
		}
	}

	if req.ContentType == "" {
		return &ValidationError{
			Field:   "content_type",
			Code:    "required",
			Message: "content type is required",
		}
	}

	if req.ContentType != ContentTypeHTML && req.ContentType != ContentTypeMarkdown {
		return &ValidationError{
			Field:   "content_type",
			Code:    "unsupported",
			Message: fmt.Sprintf("unsupported content type: supported types are [%s, %s]", ContentTypeHTML, ContentTypeMarkdown),
		}
	}

	if req.ContentType == ContentTypeMarkdown && len(req.CustomCSS) > MaxCustomCSSSize {
		return &ValidationError{
			Field:   "custom_css",
			Code:    "too_large",
			Message: fmt.Sprintf("custom CSS size %d exceeds maximum %d bytes", len(req.CustomCSS), MaxCustomCSSSize),
		}
	}

	return nil
}
