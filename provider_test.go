package document

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a minimal implementation for compile-time interface verification.
// Captures ctx to verify context propagation per Provider security contract.
// Uses mutex to guard lastCtx for -race safety if used concurrently.
type mockProvider struct {
	mu      sync.Mutex
	lastCtx context.Context
}

func (m *mockProvider) Render(ctx context.Context, _ RenderRequest) (*RenderResult, error) {
	m.mu.Lock()
	m.lastCtx = ctx
	m.mu.Unlock()
	return &RenderResult{}, nil
}

func (m *mockProvider) getLastCtx() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastCtx
}

// Compile-time assertion: mockProvider implements Provider.
var _ Provider = (*mockProvider)(nil)

func TestProviderInterfaceCompiles(t *testing.T) {
	mock := &mockProvider{}
	var p Provider = mock
	ctx := context.Background()
	result, err := p.Render(ctx, RenderRequest{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, ctx, mock.getLastCtx(), "Provider must propagate context")
}
