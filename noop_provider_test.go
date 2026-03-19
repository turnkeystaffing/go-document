package document

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNoopProviderReturnsNonNil(t *testing.T) {
	p := NewNoopProvider()
	require.NotNil(t, p)
}

func TestNoopProviderRenderReturnsValidResult(t *testing.T) {
	p := NewNoopProvider()
	result, err := p.Render(context.Background(), RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ContentTypePDF, result.ContentType)
}

func TestNoopProviderRenderReturnsNilDataAndMetadata(t *testing.T) {
	p := NewNoopProvider()
	result, err := p.Render(context.Background(), RenderRequest{
		Content:     "<p>test</p>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Data, "Data should be nil for noop provider")
	assert.Nil(t, result.Metadata, "Metadata should be nil for noop provider")
}

func TestNoopProviderRenderContextCanceled(t *testing.T) {
	p := NewNoopProvider()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := p.Render(ctx, RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	assert.Nil(t, result)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestNoopProviderRenderContextDeadlineExceeded(t *testing.T) {
	p := NewNoopProvider()
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	// Allow the deadline to expire.
	<-ctx.Done()

	result, err := p.Render(ctx, RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	assert.Nil(t, result)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestNoopProviderValidationReturnsValidationError(t *testing.T) {
	p := NewNoopProvider()
	_, err := p.Render(context.Background(), RenderRequest{})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve), "error should be *ValidationError")
	assert.Equal(t, "content", ve.Field)
	assert.Equal(t, "required", ve.Code)
}

func TestNoopProviderConcurrentRenders(t *testing.T) {
	p := NewNoopProvider()
	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make([]error, goroutines)
	results := make([]*RenderResult, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = p.Render(context.Background(), RenderRequest{
				Content:     fmt.Sprintf("<p>Test %d</p>", idx),
				ContentType: ContentTypeHTML,
				Format:      FormatPDF,
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		assert.NoError(t, errs[i], "goroutine %d should succeed", i)
		require.NotNil(t, results[i], "goroutine %d should return result", i)
		assert.Equal(t, ContentTypePDF, results[i].ContentType)
	}
}
