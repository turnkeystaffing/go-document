package document

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRenderRequestEmptyContent(t *testing.T) {
	err := ValidateRenderRequest(RenderRequest{
		Content:     "",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, "content", ve.Field)
	assert.Equal(t, "required", ve.Code)
}

func TestValidateRenderRequestContentAtExactMaxSize(t *testing.T) {
	content := strings.Repeat("a", MaxContentSize)
	err := ValidateRenderRequest(RenderRequest{
		Content:     content,
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	assert.NoError(t, err, "content at exactly MaxContentSize should pass")
}

func TestValidateRenderRequestContentExceedsMaxSize(t *testing.T) {
	content := strings.Repeat("a", MaxContentSize+1)
	err := ValidateRenderRequest(RenderRequest{
		Content:     content,
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, "content", ve.Field)
	assert.Equal(t, "too_large", ve.Code)
	assert.Contains(t, ve.Message, fmt.Sprintf("%d", MaxContentSize+1), "message should include actual size")
	assert.Contains(t, ve.Message, fmt.Sprintf("%d", MaxContentSize), "message should include maximum size")
}

func TestValidateRenderRequestEmptyContentType(t *testing.T) {
	err := ValidateRenderRequest(RenderRequest{
		Content:     "x",
		ContentType: "",
		Format:      FormatPDF,
	})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, "content_type", ve.Field)
	assert.Equal(t, "required", ve.Code)
}

func TestValidateRenderRequestUnsupportedContentType(t *testing.T) {
	err := ValidateRenderRequest(RenderRequest{
		Content:     "x",
		ContentType: "application/json",
		Format:      FormatPDF,
	})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, "content_type", ve.Field)
	assert.Equal(t, "unsupported", ve.Code)
	assert.Contains(t, ve.Message, "text/html")
	assert.Contains(t, ve.Message, "text/markdown")
}

func TestValidateRenderRequestEmptyFormat(t *testing.T) {
	err := ValidateRenderRequest(RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      "",
	})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, "format", ve.Field)
	assert.Equal(t, "required", ve.Code)
}

func TestValidateRenderRequestUnsupportedFormat(t *testing.T) {
	err := ValidateRenderRequest(RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      "png",
	})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, "format", ve.Field)
	assert.Equal(t, "unsupported", ve.Code)
}

func TestValidateRenderRequestMarkdownCustomCSSAtExactMaxSize(t *testing.T) {
	css := strings.Repeat("a", MaxCustomCSSSize)
	err := ValidateRenderRequest(RenderRequest{
		Content:     "x",
		ContentType: ContentTypeMarkdown,
		Format:      FormatPDF,
		CustomCSS:   css,
	})

	assert.NoError(t, err, "custom CSS at exactly MaxCustomCSSSize should pass")
}

func TestValidateRenderRequestMarkdownCustomCSSExceedsMaxSize(t *testing.T) {
	css := strings.Repeat("a", MaxCustomCSSSize+1)
	err := ValidateRenderRequest(RenderRequest{
		Content:     "x",
		ContentType: ContentTypeMarkdown,
		Format:      FormatPDF,
		CustomCSS:   css,
	})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, "custom_css", ve.Field)
	assert.Equal(t, "too_large", ve.Code)
	assert.Contains(t, ve.Message, fmt.Sprintf("%d", MaxCustomCSSSize+1), "message should include actual size")
	assert.Contains(t, ve.Message, fmt.Sprintf("%d", MaxCustomCSSSize), "message should include maximum size")
}

func TestValidateRenderRequestHTMLWithOversizedCustomCSSPasses(t *testing.T) {
	css := strings.Repeat("a", MaxCustomCSSSize+1)
	err := ValidateRenderRequest(RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
		CustomCSS:   css,
	})

	assert.NoError(t, err, "custom CSS should be ignored for HTML content type")
}

func TestValidateRenderRequestValidMinimal(t *testing.T) {
	err := ValidateRenderRequest(RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	assert.NoError(t, err)
}

func TestValidateRenderRequestMarkdownEmptyCustomCSS(t *testing.T) {
	err := ValidateRenderRequest(RenderRequest{
		Content:     "# Hello",
		ContentType: ContentTypeMarkdown,
		Format:      FormatPDF,
		CustomCSS:   "",
	})

	assert.NoError(t, err, "markdown with empty custom CSS should pass")
}

func TestValidateRenderRequestValidFull(t *testing.T) {
	err := ValidateRenderRequest(RenderRequest{
		Content:     "# Hello",
		ContentType: ContentTypeMarkdown,
		Format:      FormatPDF,
		CustomCSS:   "body { color: red; }",
		Options:     &RenderOptions{},
	})

	assert.NoError(t, err)
}

func TestValidationErrorErrorFormat(t *testing.T) {
	ve := &ValidationError{
		Field:   "content",
		Code:    "required",
		Message: "content is required",
	}

	assert.Equal(t, "document: validation: content: content is required", ve.Error())
}

func TestValidationErrorErrorsAs(t *testing.T) {
	var err error = &ValidationError{
		Field:   "format",
		Code:    "unsupported",
		Message: "unsupported format",
	}

	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, "format", ve.Field)
	assert.Equal(t, "unsupported", ve.Code)
}

func TestValidationConstantValues(t *testing.T) {
	assert.Equal(t, 10*1024*1024, MaxContentSize, "MaxContentSize should be 10MB")
	assert.Equal(t, 256*1024, MaxCustomCSSSize, "MaxCustomCSSSize should be 256KB")
}

func TestValidateRenderRequestValidationOrder(t *testing.T) {
	t.Run("ContentSizeBeforeFormat", func(t *testing.T) {
		// Oversized content with empty format should fail on content size (not format required),
		// confirming content size is checked before format — matches server behavior.
		content := strings.Repeat("a", MaxContentSize+1)
		err := ValidateRenderRequest(RenderRequest{
			Content:     content,
			ContentType: "",
			Format:      "",
		})

		require.Error(t, err)
		var ve *ValidationError
		require.True(t, errors.As(err, &ve))
		assert.Equal(t, "content", ve.Field, "content size should be checked before format and content_type")
		assert.Equal(t, "too_large", ve.Code)
	})

	t.Run("FormatBeforeContentType", func(t *testing.T) {
		// Valid content, empty format, empty content_type → should fail on format first.
		err := ValidateRenderRequest(RenderRequest{
			Content:     "x",
			ContentType: "",
			Format:      "",
		})

		require.Error(t, err)
		var ve *ValidationError
		require.True(t, errors.As(err, &ve))
		assert.Equal(t, "format", ve.Field, "format should be checked before content_type")
		assert.Equal(t, "required", ve.Code)
	})

	t.Run("ContentTypeBeforeCustomCSS", func(t *testing.T) {
		// Valid content, valid format, unsupported content_type, oversized CSS → should fail on content_type first.
		css := strings.Repeat("a", MaxCustomCSSSize+1)
		err := ValidateRenderRequest(RenderRequest{
			Content:     "x",
			ContentType: "application/json",
			Format:      FormatPDF,
			CustomCSS:   css,
		})

		require.Error(t, err)
		var ve *ValidationError
		require.True(t, errors.As(err, &ve))
		assert.Equal(t, "content_type", ve.Field, "content_type should be checked before custom_css")
		assert.Equal(t, "unsupported", ve.Code)
	})
}

func TestValidateRenderRequestMultiByteContentSize(t *testing.T) {
	// len(string) returns byte count, not character count. Multi-byte UTF-8
	// characters (3 bytes each for CJK) consume the limit faster than ASCII.
	// This test documents that the 10MB limit is byte-based.
	cjkChar := "\u4e16" // 世 — 3 bytes in UTF-8
	charCount := MaxContentSize / len(cjkChar)

	t.Run("JustUnderByteBoundary", func(t *testing.T) {
		// charCount = 10485760/3 = 3495253, bytes = 3495253*3 = 10485759 (1 byte under limit).
		// Integer division means we can't hit the exact boundary with 3-byte characters.
		content := strings.Repeat(cjkChar, charCount)
		assert.LessOrEqual(t, len(content), MaxContentSize, "content at charCount should be within limit")
		err := ValidateRenderRequest(RenderRequest{
			Content:     content,
			ContentType: ContentTypeHTML,
			Format:      FormatPDF,
		})
		assert.NoError(t, err, "multi-byte content within byte limit should pass")
	})

	t.Run("ExceedsByteBoundary", func(t *testing.T) {
		// charCount+1 = 3495254, bytes = 3495254*3 = 10485762 > MaxContentSize.
		content := strings.Repeat(cjkChar, charCount+1)
		require.Greater(t, len(content), MaxContentSize)
		err := ValidateRenderRequest(RenderRequest{
			Content:     content,
			ContentType: ContentTypeHTML,
			Format:      FormatPDF,
		})
		require.Error(t, err)
		var ve *ValidationError
		require.True(t, errors.As(err, &ve))
		assert.Equal(t, "content", ve.Field)
		assert.Equal(t, "too_large", ve.Code)
	})
}

func TestValidateRenderRequestCaseSensitiveFormat(t *testing.T) {
	// Format comparison is case-sensitive (req.Format != FormatPDF).
	// This test documents that behavior, parallel to CaseSensitiveContentType.
	cases := []string{"PDF", "Pdf", "pDf", "pDF"}
	for _, f := range cases {
		t.Run(f, func(t *testing.T) {
			err := ValidateRenderRequest(RenderRequest{
				Content:     "x",
				ContentType: ContentTypeHTML,
				Format:      f,
			})
			require.Error(t, err)
			var ve *ValidationError
			require.True(t, errors.As(err, &ve))
			assert.Equal(t, "format", ve.Field)
			assert.Equal(t, "unsupported", ve.Code, "case-variant %q should be treated as unsupported", f)
		})
	}
}

func TestValidateRenderRequestCaseSensitiveContentType(t *testing.T) {
	// MIME types are case-insensitive per RFC 2045, but both client and server
	// use case-sensitive comparison. This test documents that intentional behavior.
	cases := []string{"Text/HTML", "TEXT/HTML", "text/Html", "Text/Markdown", "TEXT/MARKDOWN"}
	for _, ct := range cases {
		t.Run(ct, func(t *testing.T) {
			err := ValidateRenderRequest(RenderRequest{
				Content:     "x",
				ContentType: ct,
				Format:      FormatPDF,
			})
			require.Error(t, err)
			var ve *ValidationError
			require.True(t, errors.As(err, &ve))
			assert.Equal(t, "content_type", ve.Field)
			assert.Equal(t, "unsupported", ve.Code, "case-variant %q should be treated as unsupported", ct)
		})
	}
}
