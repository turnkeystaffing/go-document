package document_test

import (
	"context"
	"fmt"

	"github.com/turnkeystaffing/go-document"
)

// providerFunc adapts a function to the Provider interface for examples.
type providerFunc func(ctx context.Context, req document.RenderRequest) (*document.RenderResult, error)

func (f providerFunc) Render(ctx context.Context, req document.RenderRequest) (*document.RenderResult, error) {
	return f(ctx, req)
}

func ExampleProvider_Render() {
	// Create a render request using SDK constants (not magic strings).
	req := document.RenderRequest{
		Content:     "<h1>Hello World</h1>",
		ContentType: document.ContentTypeHTML,
		Format:      document.FormatPDF,
	}

	// In production, use a real provider (HTTPProvider, LogProvider, etc.).
	// Here we use an inline implementation to demonstrate the call pattern.
	provider := providerFunc(func(ctx context.Context, r document.RenderRequest) (*document.RenderResult, error) {
		return &document.RenderResult{
			Data:        []byte("%PDF-mock"),
			ContentType: document.ContentTypePDF,
		}, nil
	})

	result, err := provider.Render(context.Background(), req)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("content_type=%s len=%d\n", result.ContentType, len(result.Data))
	// Output: content_type=application/pdf len=9
}

func ExampleRenderRequest() {
	// Minimal request: only required fields.
	req := document.RenderRequest{
		Content:     "# Markdown Document\n\nRendered to PDF.",
		ContentType: document.ContentTypeMarkdown,
		Format:      document.FormatPDF,
	}

	// With options: pointer fields distinguish "not set" from zero value.
	paperSize := "A4"
	orientation := "portrait"
	req.Options = &document.RenderOptions{
		PaperSize:   &paperSize,
		Orientation: &orientation,
	}

	// Pass the request to a provider with a context that carries tracing/cancellation.
	// In production code, use the context from the incoming request — never create
	// context.Background() inside service or handler code.
	_ = req

	fmt.Println("request with options created")
	// Output: request with options created
}
