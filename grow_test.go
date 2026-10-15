package rapidyenc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A declared size above yencMaxInitialAlloc is capped for the first allocation,
// but must not then be reached by doubling from the cap.
func TestGrowUsesDeclaredSizeAboveInitialCap(t *testing.T) {
	const payload = yencMaxInitialAlloc * 5

	r := &Response{}
	r.Metadata.PartSize = payload

	require.Equal(t, yencMaxInitialAlloc, r.computeExpectedSize(),
		"an unverified header must not drive the first allocation past the cap")

	// make rounds capacity up, so these are bounds rather than equalities
	r.Data = make([]byte, yencMaxInitialAlloc)
	r.grow(yencMaxInitialAlloc + 1)
	require.GreaterOrEqual(t, cap(r.Data), payload+64,
		"a corroborated declared size should be allocated in one step")
	require.Less(t, cap(r.Data), 2*(payload+64), "and it should not overshoot it")

	// A size far beyond what has been delivered stays on the doubling path
	r2 := &Response{}
	r2.Metadata.PartSize = yencMaxInitialAlloc * 1000
	r2.Data = make([]byte, yencMaxInitialAlloc)
	r2.grow(yencMaxInitialAlloc + 1)
	require.GreaterOrEqual(t, cap(r2.Data), 2*yencMaxInitialAlloc)
	require.Less(t, cap(r2.Data), 3*yencMaxInitialAlloc,
		"an uncorroborated size must not be allocated in one step")
}
