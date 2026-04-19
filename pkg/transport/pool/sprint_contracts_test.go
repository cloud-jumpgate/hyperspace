package pool_test

// sprint_contracts_test.go — tests required by sprint contracts F-004.
// Satisfies CONDITIONAL PASS → PASS for pkg/transport/pool.

import (
	"testing"

	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPool_DuplicateAdd verifies that adding a connection with the same ID twice
// does not create duplicate entries. After two Add calls with the same ID,
// Size() must remain 1.
func TestPool_DuplicateAdd(t *testing.T) {
	p := pool.New("peer", 1, 8)

	c := newMock(99)

	// First Add succeeds.
	require.NoError(t, p.Add(c), "first Add should succeed")
	assert.Equal(t, 1, p.Size(), "size should be 1 after first Add")

	// Second Add with the same connection ID is a no-op.
	require.NoError(t, p.Add(c), "second Add with same ID should not error")
	assert.Equal(t, 1, p.Size(), "size must remain 1 — no duplicate entries allowed")

	// Add a different connection — should succeed and bring size to 2.
	c2 := newMock(100)
	require.NoError(t, p.Add(c2), "Add of a different connection should succeed")
	assert.Equal(t, 2, p.Size(), "size should be 2 after adding a distinct connection")
}
