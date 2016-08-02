package optmem

import (
	"bytes"
	"math"
	"math/rand"
	"sort"
	"time"
)

const peerCompareSize = ipLen + portLen

type peerList struct {
	numSeeders   int
	numPeers     int
	numDownloads uint64
	peerBuckets  [][]peer // sorted by endpoint
}

func newPeerList() *peerList {
	return &peerList{
		peerBuckets: make([][]peer, 1),
	}
}

// TODO sort buckets by leecher/seeder?

// Returns whether at least one peer was deleted.
func (pl *peerList) collectGarbage(cutoffTime, maxDiff uint16) (gc bool) {
	for j := 0; j < len(pl.peerBuckets); j++ {
		for i := 0; i < len(pl.peerBuckets[j]); i++ {
			peer := pl.peerBuckets[j][i]
			if peer.peerTime() == cutoffTime {
				gc = true
				pl.removePeer(&peer)
				i--
			} else if peer.peerTime() < cutoffTime {
				// annoying wrapping case
				diff := uint16(math.MaxUint16) - (cutoffTime - peer.peerTime())
				if diff > maxDiff {
					gc = true
					pl.removePeer(&peer)
					i--
				}
				continue
			} else {
				diff := peer.peerTime() - cutoffTime
				if diff > maxDiff {
					gc = true
					pl.removePeer(&peer)
					i--
				}
			}
		}
	}
	return
}

// rebalanceBuckets checks if a certain number of peers is reached and performs
// rebalancing if it is.
// Rebalancing will create new buckets and redistribute all peers to them.
// Rebalancing aims to have less than 512 peers per bucket.
// Returns whether rebalancing was performed.
func (pl *peerList) rebalanceBuckets() bool {
	targetBuckets := 2
	t := 0

	if pl.numPeers > 0 {
		t = (pl.numPeers - 1) >> 9
	}
	for ; t != 0; t = t >> 1 {
		targetBuckets = targetBuckets * 2
	}

	if len(pl.peerBuckets) == targetBuckets {
		return false
	}

	before := time.Now()

	oldBuckets := pl.peerBuckets
	pl.peerBuckets = make([][]peer, targetBuckets)
	pl.numPeers = 0
	pl.numSeeders = 0

	for _, bucket := range oldBuckets {
		for _, peer := range bucket {
			pl.putPeer(&peer)
		}
	}

	if targetBuckets >= 1024 {
		logf("had to do a huge bucket rebalance to %d buckets (have %d peers) (took %s)\n", targetBuckets, pl.numPeers, time.Since(before).String())
	}
	return true
}

func (pl *peerList) removePeer(p *peer) (found bool) {
	bucketRef := &pl.peerBuckets[pl.bucketIndex(p)]
	bucket := *bucketRef
	match := sort.Search(len(bucket), func(i int) bool {
		return bytes.Compare(p.data[:peerCompareSize], bucket[i].data[:peerCompareSize]) >= 0
	})
	if match >= len(bucket) || !bytes.Equal(p.data[:peerCompareSize], bucket[match].data[:peerCompareSize]) {
		return false
	}
	found = true
	pl.numPeers--

	if bucket[match].isSeeder() {
		pl.numSeeders--
	}
	bucket = append(bucket[:match], bucket[match+1:]...)
	*bucketRef = bucket

	return
}

func (pl *peerList) putPeer(p *peer) {
	bucketRef := &pl.peerBuckets[pl.bucketIndex(p)]
	bucket := *bucketRef

	match := sort.Search(len(bucket), func(i int) bool {
		return bytes.Compare(p.data[:peerCompareSize], bucket[i].data[:peerCompareSize]) >= 0
	})
	if match >= len(bucket) || !bytes.Equal(p.data[:peerCompareSize], bucket[match].data[:peerCompareSize]) {
		// create new and insert
		bucket = append(bucket, peer{})
		copy(bucket[match+1:], bucket[match:])
		bucket[match] = *p
		*bucketRef = bucket
		pl.numPeers++
		if p.isSeeder() {
			pl.numSeeders++
		}
		return
	}

	// update existing
	// update seeder/leecher count!
	if bucket[match].isLeecher() && p.isSeeder() {
		pl.numSeeders++
	} else if bucket[match].isSeeder() && p.isLeecher() {
		// strange case but whatever
		pl.numSeeders--
	}
	bucket[match] = *p

	return
}

func (pl *peerList) getAllPeers() []peer {
	buckets := pl.peerBuckets
	seeders := make([]peer, 0, pl.numSeeders)
	leechers := make([]peer, 0, pl.numPeers) // will be reused to store all peers, hence the size

	for _, b := range buckets {
		for _, peer := range b {
			if peer.isSeeder() {
				seeders = append(seeders, peer) // will never realloc
			} else {
				leechers = append(leechers, peer) // will never realloc
			}
		}
	}

	// leechers are first, then seeders
	if len(seeders) > 0 {
		leechers = append(leechers, seeders...) // will never realloc
	}

	return leechers
}

func (pl *peerList) getAllSeeders() []peer {
	buckets := pl.peerBuckets
	seeders := make([]peer, 0, pl.numSeeders)

	for _, b := range buckets {
		for _, peer := range b {
			if peer.isSeeder() {
				seeders = append(seeders, peer)
			}
		}
	}

	return seeders
}

func (pl *peerList) getAllLeechers() []peer {
	buckets := pl.peerBuckets
	leechers := make([]peer, 0, pl.numPeers-pl.numSeeders)

	for _, b := range buckets {
		for _, peer := range b {
			if peer.isLeecher() {
				leechers = append(leechers, peer)
			}
		}
	}

	return leechers
}

func (pl *peerList) getRandomSeeders(numWant int, r *rand.Rand) []peer {
	buckets := pl.peerBuckets
	toReturn := make([]peer, numWant)
	chosen := 0

	if numWant == 0 {
		return toReturn
	}

	for chosen < numWant {
		bucketOffset := r.Int()
		for _, b := range buckets {
			if chosen == numWant {
				break
			}
			if len(b) == 0 {
				continue
			}
			peer := b[bucketOffset%len(b)]
			if peer.isSeeder() {
				toReturn[chosen] = peer
				chosen++
			}
		}
	}

	return toReturn
}

func (pl *peerList) getRandomLeechers(numWant int, r *rand.Rand) []peer {
	buckets := pl.peerBuckets
	toReturn := make([]peer, numWant)
	chosen := 0

	if numWant == 0 {
		return toReturn
	}

	for chosen < numWant {
		bucketOffset := r.Int()
		for _, b := range buckets {
			if chosen == numWant {
				break
			}
			if len(b) == 0 {
				continue
			}
			peer := b[bucketOffset%len(b)]
			if peer.isLeecher() {
				toReturn[chosen] = peer
				chosen++
			}
		}
	}

	return toReturn
}

func (pl *peerList) getAnnouncePeers(numWant int, seeder bool, announcingPeer *peer, r *rand.Rand) (peers []peer) {
	if seeder {
		// seeder announces: only leechers
		if numWant > pl.numPeers-pl.numSeeders {
			numWant = pl.numPeers - pl.numSeeders
		}
		if numWant == pl.numPeers-pl.numSeeders {
			return pl.getAllLeechers()
		}
		return pl.getRandomLeechers(numWant, r)
	}

	// leecher announces: seeders as many as possible, then leechers

	if numWant > pl.numPeers {
		// we can only return as many peers as we have
		numWant = pl.numPeers
	}

	// we have enough seeders to only return seeders
	if numWant <= pl.numSeeders {
		return pl.getRandomSeeders(numWant, r)
	}
	// we have exactly as many peers as they want
	if numWant == pl.numPeers {
		tmp := pl.getAllPeers()
		peers = make([]peer, 0, len(tmp))
		for _, p := range tmp {
			// filter out the announcing peer
			if !bytes.Equal(p.data[:peerCompareSize], announcingPeer.data[:peerCompareSize]) {
				peers = append(peers, p)
			}
		}
		return
	}

	// we don't have enough seeders to only return seeders
	peers = make([]peer, 0, numWant)
	peers = append(peers, pl.getAllSeeders()...)
	leechers := pl.getRandomLeechers(numWant-len(peers), r)
	for _, p := range leechers {
		// filter out the announcing peer
		if !bytes.Equal(p.data[:peerCompareSize], announcingPeer.data[:peerCompareSize]) {
			peers = append(peers, p)
		}
	}
	return
}

func (pl *peerList) bucketIndex(peer *peer) int {
	var hash uint = 5381
	var i uint = peerCompareSize

	for j := 0; i > 0; i, j = i-1, j+1 {
		hash += (hash << 5) + uint(peer.data[j])
	}

	return int(hash % uint(len(pl.peerBuckets)))
}
