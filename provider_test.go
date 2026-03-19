package document

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a minimal implementation for compile-time interface verification.
// Captures ctx to verify context propagation per Provider security contract.
type mockProvider struct {
	lastCtx context.Context
}

func (m *mockProvider) Render(ctx context.Context, _ RenderRequest) (*RenderResult, error) {
	m.lastCtx = ctx
	return &RenderResult{}, nil
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
	assert.Equal(t, ctx, mock.lastCtx, "Provider must propagate context")
}
