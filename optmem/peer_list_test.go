package optmem

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var computTargetBucketsData = []struct{ numPeers, expectedTargetBuckets, expectedDefensiveBuckets int }{
	{256, 1, 1},
	{512, 1, 2},
	{513, 2, 2},
	{1024, 2, 4},
	{1025, 4, 4},
}

func TestComputeTargetBuckets(t *testing.T) {
	for _, c := range computTargetBucketsData {
		got, defensiveGot := computeTargetBuckets(c.numPeers)
		require.Equal(t, c.expectedTargetBuckets, got)
		require.Equal(t, c.expectedDefensiveBuckets, defensiveGot)
	}
}
