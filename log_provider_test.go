package document

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordStore is the shared storage for recordHandler instances.
// Child handlers created via WithAttrs/WithGroup share the same store.
type recordStore struct {
	mu      sync.Mutex
	records []slog.Record
}

// recordHandler captures slog.Record entries for structured test assertions.
// Thread-safe via shared recordStore — safe for concurrent LogProvider tests under -race.
// Supports WithAttrs/WithGroup: child handlers prepend stored attrs to each record.
type recordHandler struct {
	store    *recordStore
	preAttrs []slog.Attr // attrs added via WithAttrs
}

func (h *recordHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	clone := r.Clone()
	if len(h.preAttrs) > 0 {
		clone.AddAttrs(h.preAttrs...)
	}
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	h.store.records = append(h.store.records, clone)
	return nil
}

func (h *recordHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := make([]slog.Attr, len(h.preAttrs), len(h.preAttrs)+len(attrs))
	copy(combined, h.preAttrs)
	combined = append(combined, attrs...)
	return &recordHandler{store: h.store, preAttrs: combined}
}

func (h *recordHandler) WithGroup(_ string) slog.Handler {
	// Group support not needed for current tests — return self.
	return h
}

func (h *recordHandler) lastRecord(t *testing.T) slog.Record {
	t.Helper()
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	if len(h.store.records) == 0 {
		t.Fatal("recordHandler: lastRecord called with no records captured")
	}
	return h.store.records[len(h.store.records)-1]
}

func (h *recordHandler) count() int {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	return len(h.store.records)
}

// attrMap converts a Record's attrs to a map for convenient assertions.
func attrMap(r slog.Record) map[string]slog.Value {
	m := make(map[string]slog.Value)
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value
		return true
	})
	return m
}

// recordContainsString checks if any attr value string representation contains s.
func recordContainsString(r slog.Record, s string) bool {
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if strings.Contains(a.Value.String(), s) {
			found = true
			return false
		}
		return true
	})
	return found
}

func newRecordCaptureProvider(t *testing.T) (*LogProvider, *recordHandler) {
	t.Helper()
	h := &recordHandler{store: &recordStore{}}
	return NewLogProvider(slog.New(h)), h
}

func TestLogProviderRenderLogsCorrectMessage(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	_, err := p.Render(context.Background(), RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	require.Equal(t, 1, h.count())
	assert.Equal(t, "document render request", h.lastRecord(t).Message)
}

func TestLogProviderRenderLogsStructuredFields(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	_, err := p.Render(context.Background(), RenderRequest{
		Content:     "<h1>Hello</h1>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	attrs := attrMap(h.lastRecord(t))
	assert.Equal(t, "text/html", attrs["content_type"].String())
	assert.Equal(t, "pdf", attrs["format"].String())
	assert.Equal(t, int64(14), attrs["content_size"].Int64())
	assert.Equal(t, false, attrs["has_options"].Bool())
	assert.Equal(t, false, attrs["has_custom_css"].Bool())
}

func TestLogProviderRenderContentSizeReflectsActualLength(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	content := "short"
	_, err := p.Render(context.Background(), RenderRequest{
		Content:     content,
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	attrs := attrMap(h.lastRecord(t))
	assert.Equal(t, int64(len(content)), attrs["content_size"].Int64())
}

func TestLogProviderRenderHasOptionsTrue(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	_, err := p.Render(context.Background(), RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
		Options:     &RenderOptions{},
	})

	require.NoError(t, err)
	attrs := attrMap(h.lastRecord(t))
	assert.Equal(t, true, attrs["has_options"].Bool())
}

func TestLogProviderRenderHasOptionsFalse(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	_, err := p.Render(context.Background(), RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
		Options:     nil,
	})

	require.NoError(t, err)
	attrs := attrMap(h.lastRecord(t))
	assert.Equal(t, false, attrs["has_options"].Bool())
}

func TestLogProviderRenderHasCustomCSSTrue(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	_, err := p.Render(context.Background(), RenderRequest{
		Content:     "x",
		ContentType: ContentTypeMarkdown,
		Format:      FormatPDF,
		CustomCSS:   "body { color: red; }",
	})

	require.NoError(t, err)
	attrs := attrMap(h.lastRecord(t))
	assert.Equal(t, true, attrs["has_custom_css"].Bool())
}

func TestLogProviderRenderHasCustomCSSFalse(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	_, err := p.Render(context.Background(), RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
		CustomCSS:   "",
	})

	require.NoError(t, err)
	attrs := attrMap(h.lastRecord(t))
	assert.Equal(t, false, attrs["has_custom_css"].Bool())
}

func TestLogProviderRenderDoesNotLogContent(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	secretContent := "<h1>SECRET_PII_DATA_12345</h1>"
	_, err := p.Render(context.Background(), RenderRequest{
		Content:     secretContent,
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	require.Equal(t, 1, h.count())
	r := h.lastRecord(t)
	assert.NotContains(t, r.Message, "SECRET_PII_DATA_12345", "content must not appear in log message")
	assert.False(t, recordContainsString(r, "SECRET_PII_DATA_12345"), "content must not appear in any log attribute (PII safety)")
	assert.False(t, recordContainsString(r, "<h1>"), "HTML content must not appear in any log attribute")
}

func TestLogProviderRenderDoesNotLogCustomCSSContent(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	secretCSS := ".secret-class-xyz { display: none; }"
	_, err := p.Render(context.Background(), RenderRequest{
		Content:     "x",
		ContentType: ContentTypeMarkdown,
		Format:      FormatPDF,
		CustomCSS:   secretCSS,
	})

	require.NoError(t, err)
	require.Equal(t, 1, h.count())
	assert.False(t, recordContainsString(h.lastRecord(t), "secret-class-xyz"), "CSS content must not be logged")
}

func TestLogProviderRenderReturnsValidResult(t *testing.T) {
	p, _ := newRecordCaptureProvider(t)

	result, err := p.Render(context.Background(), RenderRequest{
		Content:     "<p>test</p>",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ContentTypePDF, result.ContentType)
	assert.Nil(t, result.Data, "Data should be nil for log provider")
	assert.Nil(t, result.Metadata, "Metadata should be nil for log provider")
}

func TestLogProviderRenderContextCanceledNoLog(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

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
	assert.Equal(t, 0, h.count(), "no log entry should be written for cancelled requests")
}

func TestLogProviderRenderContextDeadlineExceeded(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-ctx.Done()

	result, err := p.Render(ctx, RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})

	assert.Nil(t, result)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.Equal(t, 0, h.count(), "no log entry should be written for deadline-exceeded requests")
}

func TestNewLogProviderNilLoggerDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		p := NewLogProvider(nil)
		require.NotNil(t, p)
	})
}

func TestNewLogProviderNilLoggerUsesDefault(t *testing.T) {
	// Replace the default logger with a capturing handler to verify logs are written.
	h := &recordHandler{store: &recordStore{}}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })

	p := NewLogProvider(nil)
	result, err := p.Render(context.Background(), RenderRequest{
		Content:     "x",
		ContentType: ContentTypeHTML,
		Format:      FormatPDF,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, h.count(), "nil logger should log via slog.Default()")
	assert.Equal(t, "document render request", h.lastRecord(t).Message)
}

func TestLogProviderValidationBeforeLogging(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

	_, err := p.Render(context.Background(), RenderRequest{})

	require.Error(t, err)
	var ve *ValidationError
	require.True(t, errors.As(err, &ve), "error should be *ValidationError")
	assert.Equal(t, "content", ve.Field)
	assert.Equal(t, "required", ve.Code)
	assert.Equal(t, 0, h.count(), "no log entry should be written for invalid input")
}

func TestLogProviderConcurrentRenders(t *testing.T) {
	p, h := newRecordCaptureProvider(t)

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
	}

	assert.Equal(t, goroutines, h.count(), "each goroutine should produce a log entry")
}
