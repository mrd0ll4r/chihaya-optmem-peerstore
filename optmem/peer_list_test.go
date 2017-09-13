package optmem

import (
	"bytes"
	"fmt"
	"net"
	"sort"
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

func TestPutPeer(t *testing.T) {
	pl := newPeerList()
	for i := 0; i < 10; i++ {
		p := new(peer)
		p.setIP(net.IP{245, 132, 24, byte(i)}.To16())
		p.setPort(3124 + uint16(i))
		pl.putPeer(p)
	}

	t.Log("Bucket after all inserts", pl.peerBuckets[0])

	for i := range pl.peerBuckets[0] {
		if i > 0 {
			require.True(t, bytes.Compare(pl.peerBuckets[0][i-1][:peerCompareSize], pl.peerBuckets[0][i][:peerCompareSize]) == -1)
		}
	}
}

func TestRemovePeer(t *testing.T) {
	pl := newPeerList()
	for i := 0; i < 10; i++ {
		p := new(peer)
		p.setIP(net.IP{245, 132, 24, byte(i)}.To16())
		p.setPort(3124 + uint16(i))
		pl.putPeer(p)
	}

	t.Log("Bucket after all inserts", pl.peerBuckets[0])

	for i := 0; i < 10; i++ {
		p := new(peer)
		p.setIP(net.IP{245, 132, 24, byte(i)}.To16())
		p.setPort(3124 + uint16(i))
		found, _ := pl.removePeer(p)
		require.True(t, found)
	}

	require.Equal(t, 0, len(pl.peerBuckets[0]))
}

func BenchmarkRebalanceBuckets(b *testing.B) {
	for k := 2; k < 10; k *= 2 {
		b.Run(fmt.Sprintf("%d-peers-to-%d-buckets", 512*k, k), func(b *testing.B) {
			pl := newPeerList()
			numPeers := 0
			for j := 0; j < k*2; j++ {
				for i := 0; i < 256; i++ {
					p := peer{}
					p.setIP(net.IP{245, 132, byte(j), byte(i)}.To16())
					p.setPort(3142 + uint16(numPeers))
					pl.peerBuckets[0] = append(pl.peerBuckets[0], p)
					numPeers++
				}
			}
			pl.numPeers = numPeers
			require.Equal(b, 512*k, numPeers)

			oldBucket := pl.peerBuckets[0]

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pl.peerBuckets = []bucket{oldBucket}
				rebalanced := pl.rebalanceBuckets()
				require.True(b, rebalanced)
			}
		})
	}

}

func TestRebalanceBuckets(t *testing.T) {
	pl := newPeerList()
	pl2 := newPeerList()
	numPeers := 0
	for j := 0; j < 10; j++ {
		for i := 0; i < 256; i++ {
			p := peer{}
			p.setIP(net.IP{245, 132, byte(j), byte(i)}.To16())
			p.setPort(3142 + uint16(numPeers))
			pl.peerBuckets[0] = append(pl.peerBuckets[0], p)
			pl2.putPeer(&p)
			numPeers++
		}
	}
	pl.numPeers = numPeers

	done := pl.rebalanceBuckets()
	require.True(t, done)
	require.Equal(t, 8, len(pl.peerBuckets))
	done = pl2.rebalanceBuckets()
	require.True(t, done)
	require.Equal(t, 8, len(pl2.peerBuckets))

	for j := range pl.peerBuckets {
		t.Logf("Bucket %d has %d peers", j, len(pl.peerBuckets[j]))
		for i := range pl.peerBuckets[j] {
			require.Equal(t, pl.peerBuckets[j][i], pl2.peerBuckets[j][i])
			if i > 0 {
				require.True(t, bytes.Compare(pl.peerBuckets[j][i-1][:peerCompareSize], pl.peerBuckets[j][i][:peerCompareSize]) == -1)
			}
			// test if we can find the peer with binary search
			require.True(t, pl.findPeer(&pl.peerBuckets[j][i]))
		}
	}
}

func (pl *peerList) findPeer(p *peer) bool {
	bucketRef := &pl.peerBuckets[pl.bucketIndex(p)]
	bucket := *bucketRef

	match := sort.Search(len(bucket), binarySearchFunc(p, bucket))
	if match >= len(bucket) || !bytes.Equal(p[:peerCompareSize], bucket[match][:peerCompareSize]) {
		return false
	}
	return true
}
