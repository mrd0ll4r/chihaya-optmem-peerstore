package optmem

import (
	"bytes"
	"fmt"
	"math"
	"net"
	"sort"
	"time"

	"github.com/chihaya/chihaya/middleware/pkg/random"
	"github.com/chihaya/chihaya/pkg/log"
)

const peerCompareSize = ipLen + portLen

type peerList struct {
	numSeeders   int
	numPeers     int
	numDownloads uint64
	peerBuckets  []bucket // sorted by endpoint
}

type bucket []peer

// Len implements sort.Interface for a bucket.
func (b bucket) Len() int {
	return len(b)
}

// Less implements sort.Interface for a bucket.
func (b bucket) Less(i, j int) bool {
	return bytes.Compare(b[i][:peerCompareSize], b[j][:peerCompareSize]) < 0
}

// Swap implements sort.Interface for a bucket.
func (b bucket) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func newPeerList() *peerList {
	return &peerList{
		peerBuckets: make([]bucket, 1),
	}
}

// TODO sort buckets by leecher/seeder?

// Returns whether at least one peer was deleted.
func (pl *peerList) collectGarbage(cutoffTime, maxDiff uint16) (gc bool) {
	for j := 0; j < len(pl.peerBuckets); j++ {
		for i := 0; i < len(pl.peerBuckets[j]); i++ {
			peer := pl.peerBuckets[j][i]
			var remove bool
			if peer.peerTime() == cutoffTime {
				remove = true
			} else if peer.peerTime() < cutoffTime {
				// annoying wrapping case
				diff := uint16(math.MaxUint16) - (cutoffTime - peer.peerTime())
				if diff > maxDiff {
					remove = true
				}
			} else {
				diff := peer.peerTime() - cutoffTime
				if diff > maxDiff {
					remove = true
				}
			}
			if remove {
				gc = true
				found := pl.removePeer(&peer)
				if !found {
					panic(fmt.Sprintf("peer not found during GC, peer: %s %d", net.IP(peer.ip()), peer.port()))
				}
				i--
			}
		}
	}
	return
}

// computeTargetBuckets computes the number of buckets to be used for a number
// of peers.
// It returns targetBuckets and defensiveTargetBuckets, to be used when reducing
// the number of peers.
// computeTargetBuckets aims to have <=512 peers per bucket, assuming an even
// distribution.
// A buffer of numPeers/10 is used to avoid churn when the number of peers is
// hovering around one of 512^k, for example 512.
// See rebalanceBuckets for a usage example.
func computeTargetBuckets(numPeers int) (int, int) {
	targetBuckets := 1
	defensiveTargetBuckets := 1
	bufferWidth := numPeers/10 - 1

	if numPeers > 0 {
		for t := (numPeers - 1) >> 9; t != 0; t = t >> 1 {
			targetBuckets = targetBuckets * 2
		}
		for t := (numPeers + bufferWidth) >> 9; t != 0; t = t >> 1 {
			defensiveTargetBuckets = defensiveTargetBuckets * 2
		}
	}

	return targetBuckets, defensiveTargetBuckets
}

// rebalanceBuckets checks if a certain number of peers is reached and performs
// rebalancing if it is.
// Rebalancing will create new buckets and redistribute all peers to them. It
// aims to have less than 512 peers per bucket.
// When more buckets are necessary to fulfill <=512 peers per bucket, they will
// be created immediately and peers will be redistributed.
// On the other hand, if less buckets could sustain the <=512 target, there is
// a buffer zone of pl.numPeers/10 peers, to avoid sizing the bucket list up and
// down constantly.
// Returns whether rebalancing was performed.
func (pl *peerList) rebalanceBuckets() bool {
	targetBuckets, defensiveTargetBuckets := computeTargetBuckets(pl.numPeers)

	if len(pl.peerBuckets) == targetBuckets {
		return false
	} else if len(pl.peerBuckets) > targetBuckets {
		if targetBuckets != defensiveTargetBuckets {
			// Buffer zone: don't immediately reduce the number of buckets to reduce churn
			return false
		}
	}

	before := time.Now()
	oldBuckets := pl.peerBuckets
	pl.peerBuckets = make([]bucket, targetBuckets)

	// Add all peers to their buckets, without explicitly sorting them.
	// This should avoid a lot of memmoves.
	for _, bucket := range oldBuckets {
		for _, peer := range bucket {
			bucketRef := &pl.peerBuckets[pl.bucketIndex(&peer)]
			*bucketRef = append(*bucketRef, peer)
		}
	}
	// (Quick)Sort them. Just swapping pointers, should be fast (I hope).
	for _, bucket := range pl.peerBuckets {
		sort.Sort(bucket)
	}

	log.Debug("optmem: bucket rebalance finished", log.Fields{"buckets": targetBuckets, "numPeers": pl.numPeers, "timeTaken": time.Since(before)})
	if targetBuckets >= 256 {
		log.Info("optmem: had to do a huge bucket rebalance", log.Fields{"buckets": targetBuckets, "numPeers": pl.numPeers, "timeTaken": time.Since(before)})
	}
	return true
}

func binarySearchFunc(p *peer, b bucket) func(int) bool {
	return func(i int) bool {
		return bytes.Compare(p[:peerCompareSize], b[i][:peerCompareSize]) <= 0
	}
}

func (pl *peerList) removePeer(p *peer) (found bool) {
	bucketRef := &pl.peerBuckets[pl.bucketIndex(p)]
	bucket := *bucketRef
	match := sort.Search(len(bucket), binarySearchFunc(p, bucket))
	if match >= len(bucket) || bucket[match].peerFlag() != p.peerFlag() || !bytes.Equal(p[:peerCompareSize], bucket[match][:peerCompareSize]) {
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
	match := sort.Search(len(bucket), binarySearchFunc(p, bucket))
	if match >= len(bucket) || !bytes.Equal(p[:peerCompareSize], bucket[match][:peerCompareSize]) {
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

func (pl *peerList) getRandomSeeders(numWant int, s0, s1 uint64) []peer {
	buckets := pl.peerBuckets
	toReturn := make([]peer, numWant)
	chosen := 0

	if numWant == 0 {
		return toReturn
	}

	bucketOffset := 0
	for chosen < numWant {
		bucketOffset, s0, s1 = random.Intn(s0, s1, 1024)
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

func (pl *peerList) getRandomLeechers(numWant int, s0, s1 uint64) []peer {
	buckets := pl.peerBuckets
	toReturn := make([]peer, numWant)
	chosen := 0

	if numWant == 0 {
		return toReturn
	}

	bucketOffset := 0
	for chosen < numWant {
		bucketOffset, s0, s1 = random.Intn(s0, s1, 1024)
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

func (pl *peerList) getAnnouncePeers(numWant int, seeder bool, announcingPeer *peer, s0, s1 uint64) (peers []peer) {
	if seeder {
		// seeder announces: only leechers
		if numWant > pl.numPeers-pl.numSeeders {
			numWant = pl.numPeers - pl.numSeeders
		}
		if numWant == pl.numPeers-pl.numSeeders {
			return pl.getAllLeechers()
		}
		return pl.getRandomLeechers(numWant, s0, s1)
	}

	// leecher announces: seeders as many as possible, then leechers

	if numWant > pl.numPeers {
		// we can only return as many peers as we have
		numWant = pl.numPeers
	}

	// we have enough seeders to only return seeders
	if numWant <= pl.numSeeders {
		return pl.getRandomSeeders(numWant, s0, s1)
	}
	// we have exactly as many peers as they want
	if numWant == pl.numPeers {
		peers = pl.getAllPeers()
		return
	}

	// we don't have enough seeders to only return seeders
	peers = make([]peer, 0, numWant)
	peers = append(peers, pl.getAllSeeders()...)
	leechers := pl.getRandomLeechers(numWant-len(peers), s0, s1)
	peers = append(peers, leechers...)
	return
}

func (pl *peerList) bucketIndex(peer *peer) int {
	var hash uint = 5381
	var i uint = peerCompareSize

	for j := 0; i > 0; i, j = i-1, j+1 {
		hash += (hash << 5) + uint(peer[j])
	}

	return int(hash % uint(len(pl.peerBuckets)))
}
