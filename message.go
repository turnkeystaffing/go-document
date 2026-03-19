package document

// Supported content types for render requests and responses.
const (
	ContentTypeHTML     = "text/html"
	ContentTypeMarkdown = "text/markdown"
	// ContentTypePDF is the MIME type returned in RenderResult.ContentType for PDF output.
	ContentTypePDF = "application/pdf"
)

// Output format constants.
const (
	// FormatPDF is the output format for PDF rendering.
	FormatPDF = "pdf"
)

// Well-known metadata keys returned in RenderResult.Metadata.
// Values match the gateway's RenderResponseMetadata JSON field names.
const (
	MetadataKeyPages            = "pages"
	MetadataKeyRenderDurationMs = "render_duration_ms"
	// MetadataKeyBlockedResources may be absent from gateway responses when zero
	// (gateway uses omitempty on this field; Pages and RenderDurationMs are always present).
	MetadataKeyBlockedResources = "blocked_resources"
)

// RenderRequest represents a document render request.
// JSON tags match the HTTP API contract (POST /v1/render).
type RenderRequest struct {
	// Content is the raw HTML or Markdown to render. The server enforces a maximum
	// size of 10MB. Content may contain sensitive data and should not be logged.
	Content string `json:"content"`
	// ContentType is the content MIME type: use ContentTypeHTML ("text/html")
	// or ContentTypeMarkdown ("text/markdown").
	ContentType string `json:"content_type"`
	// Format is the desired output format: use FormatPDF ("pdf").
	Format string `json:"format"`
	// CustomCSS is optional CSS applied during Markdown rendering (ignored for HTML).
	// The server enforces a maximum size of 256KB. Note: CSS is injected into a <style>
	// element server-side. The server sanitizes against </style> breakout but does not
	// parse or filter CSS directives. Ensure the server's network interceptor is properly
	// configured to block external resource requests from CSS @import or url() rules.
	// SDK uses omitempty (empty string omitted); gateway DTO does not (empty string sent).
	CustomCSS string `json:"custom_css,omitempty"`
	// Options specifies page layout configuration. nil means "use all server defaults".
	// A non-nil empty &RenderOptions{} is serialized as "options":{} in JSON.
	Options *RenderOptions `json:"options,omitempty"`
}

// MaxResultDataSize is the maximum expected size of RenderResult.Data (decoded PDF bytes).
// Matches the server's 50MB output cap. Provider implementations should use this constant
// with io.LimitReader or equivalent to prevent OOM from oversized server responses.
const MaxResultDataSize = 50 * 1024 * 1024 // 50MB

// RenderResult represents the output of a successful render.
//
// Data is []byte (raw PDF bytes) and may contain sensitive rendered content
// (PII, financial, legal documents). Callers should not log Data, and should
// consider encrypting it at rest. The gateway's RenderResponseDTO uses string
// (base64-encoded). JSON marshaling handles []byte ↔ base64 transparently,
// but HTTP provider implementations must account for the gateway's typed
// metadata struct vs this type's map[string]string when deserializing.
// The server caps PDF output at 50MB.
type RenderResult struct {
	Data []byte `json:"data"`
	// ContentType is the MIME type of the rendered output (e.g., ContentTypePDF).
	ContentType string `json:"content_type"`
	// Metadata contains extensible key-value pairs. Well-known keys are defined as
	// MetadataKey* constants. Note: the gateway's RenderResponseDTO always includes
	// metadata (no omitempty); SDK omits it when nil/empty for cleaner serialization.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RenderOptions specifies page layout options for PDF rendering.
// All fields use pointer types to distinguish "not specified" (nil) from zero value.
// nil Options on RenderRequest means "use all server defaults".
type RenderOptions struct {
	PaperSize    *string `json:"paper_size,omitempty"`
	Orientation  *string `json:"orientation,omitempty"`
	MarginTop    *string `json:"margin_top,omitempty"`
	MarginBottom *string `json:"margin_bottom,omitempty"`
	MarginLeft   *string `json:"margin_left,omitempty"`
	MarginRight  *string `json:"margin_right,omitempty"`
	// Scale is the page scale factor (0.1 to 2.0). NaN and Infinity values will
	// cause json.Marshal to return an error.
	Scale *float64 `json:"scale,omitempty"`
	// PrintBackground controls whether CSS backgrounds are included in the PDF.
	PrintBackground *bool `json:"print_background,omitempty"`
}
