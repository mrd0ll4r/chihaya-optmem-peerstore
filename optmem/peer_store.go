package optmem

import (
	"encoding/binary"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/chihaya/chihaya/bittorrent"
	"github.com/chihaya/chihaya/pkg/log"
	"github.com/chihaya/chihaya/pkg/stop"
	"github.com/chihaya/chihaya/pkg/timecache"
	"github.com/chihaya/chihaya/storage"
	"github.com/pkg/errors"
)

// ErrInvalidIP is returned if a peer with an invalid IP was specified.
var ErrInvalidIP = errors.New("invalid IP")

var _ storage.PeerStore = &PeerStore{}

// New creates a new PeerStore from the config.
func New(provided Config) (*PeerStore, error) {
	cfg := provided.Validate()

	ps := &PeerStore{
		shards: newShardContainer(cfg.ShardCountBits),
		closed: make(chan struct{}),
		cfg:    cfg,
	}

	// Start a goroutine for garbage collection.
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		for {
			select {
			case <-ps.closed:
				return
			case <-time.After(cfg.GarbageCollectionInterval):
				cutoffTime := time.Now().Add(cfg.PeerLifetime * -1)
				log.Debug("optmem: collecting garbage", log.Fields{"cutoffTime": cutoffTime})
				ps.collectGarbage(cutoffTime)
				log.Debug("optmem: finished collecting garbage")
			}
		}
	}()

	// Start a goroutine for reporting statistics to Prometheus.
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		t := time.NewTicker(cfg.PrometheusReportingInterval)
		for {
			select {
			case <-ps.closed:
				t.Stop()
				return
			case <-t.C:
				before := time.Now()
				log.Debug("optmem: populating prometheus...")
				ps.populateProm()
				log.Debug("storage: populateProm() finished", log.Fields{"timeTaken": time.Since(before)})
			}
		}
	}()

	return ps, nil
}

// PeerStore is an instance of an optmem PeerStore.
type PeerStore struct {
	shards *shardContainer
	closed chan struct{}
	cfg    Config
	wg     sync.WaitGroup
}

// recordGCDuration records the duration of a GC sweep.
func recordGCDuration(duration time.Duration) {
	storage.PromGCDurationMilliseconds.Observe(float64(duration.Nanoseconds()) / float64(time.Millisecond))
}

// populateProm aggregates metrics over all shards and then posts them to
// prometheus.
func (s *PeerStore) populateProm() {
	storage.PromInfohashesCount.Set(float64(s.NumSwarms()))
	seeders, leechers := s.NumTotalPeers()
	storage.PromSeedersCount.Set(float64(seeders))
	storage.PromLeechersCount.Set(float64(leechers))
}

// LogFields implements log.LogFielder for a PeerStore.
func (s *PeerStore) LogFields() log.Fields {
	return s.cfg.LogFields()
}

func (s *PeerStore) collectGarbage(cutoff time.Time) {
	start := time.Now()
	internalCutoff := uint16(cutoff.Unix())
	maxDiff := uint16(time.Now().Unix() - cutoff.Unix())
	seeders, leechers := s.NumTotalPeers()
	log.Debug("optmem: running GC", log.Fields{"internalCutoff": internalCutoff, "maxDiff": maxDiff, "numInfohashes": s.NumSwarms(), "numPeers": seeders + leechers})

	for i := 0; i < len(s.shards.shards); i++ {
		deltaTorrents := 0
		// We must recount the number of seeders/leechers during GC, that's probably easier than having
		// (*peerList).collectGarbage() return the number.
		var numPeers, numSeeders uint64
		log.Debug("garbage-collecting shard", log.Fields{"index": i})
		shard := s.shards.lockShard(i)
		log.Debug("got GC lock", log.Fields{"index": i, "infohashesInShard": len(shard.swarms)})

		for ih, s := range shard.swarms {
			if s.peers4 != nil {
				gc := s.peers4.collectGarbage(internalCutoff, maxDiff)
				if s.peers4.numPeers == 0 {
					s.peers4 = nil
					shard.swarms[ih] = s
				} else {
					if gc {
						s.peers4.rebalanceBuckets()
					}
					numPeers += uint64(s.peers4.numPeers)
					numSeeders += uint64(s.peers4.numSeeders)
				}
			}

			if s.peers6 != nil {
				gc := s.peers6.collectGarbage(internalCutoff, maxDiff)
				if s.peers6.numPeers == 0 {
					s.peers6 = nil
					shard.swarms[ih] = s
				} else {
					if gc {
						s.peers6.rebalanceBuckets()
					}
					numPeers += uint64(s.peers6.numPeers)
					numSeeders += uint64(s.peers6.numSeeders)
				}
			}

			if s.peers4 == nil && s.peers6 == nil {
				delete(shard.swarms, ih)
				deltaTorrents--
			}
		}

		shard.numPeers = numPeers
		shard.numSeeders = numSeeders

		s.shards.unlockShard(i, deltaTorrents)
		log.Debug("done garbage-collecting shard", log.Fields{"index": i})
		runtime.Gosched()
	}

	recordGCDuration(time.Since(start))
	seeders, leechers = s.NumTotalPeers()
	log.Debug("optmem: GC done", log.Fields{"numInfohashes": s.NumSwarms(), "numPeers": seeders + leechers})
}

// CollectGarbage can be used to manually collect peers older than the given
// cutoff.
func (s *PeerStore) CollectGarbage(cutoff time.Time) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	s.collectGarbage(cutoff)
	return nil
}

// PutSeeder implements the PutSeeder method of a storage.PeerStore.
func (s *PeerStore) PutSeeder(infoHash bittorrent.InfoHash, p bittorrent.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	peer := makePeer(p, peerFlagSeeder, uint16(timecache.NowUnix()))
	ih := infohash(infoHash)

	s.putPeer(ih, peer, p.IP.AddressFamily)

	return nil
}

// DeleteSeeder implements the DeleteSeeder method of a storage.PeerStore.
func (s *PeerStore) DeleteSeeder(infoHash bittorrent.InfoHash, p bittorrent.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	peer := makePeer(p, peerFlagSeeder, uint16(0))
	ih := infohash(infoHash)

	_, err := s.deletePeer(ih, peer, p.IP.AddressFamily)

	return err
}

// PutLeecher implements the PutLeecher method of a storage.PeerStore.
func (s *PeerStore) PutLeecher(infoHash bittorrent.InfoHash, p bittorrent.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	peer := makePeer(p, peerFlagLeecher, uint16(timecache.NowUnix()))
	ih := infohash(infoHash)

	s.putPeer(ih, peer, p.IP.AddressFamily)

	return nil
}

// DeleteLeecher implements the DeleteLeecher method of a storage.PeerStore.
func (s *PeerStore) DeleteLeecher(infoHash bittorrent.InfoHash, p bittorrent.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	peer := makePeer(p, peerFlagLeecher, uint16(0))
	ih := infohash(infoHash)

	_, err := s.deletePeer(ih, peer, p.IP.AddressFamily)

	return err
}

// GraduateLeecher implements the GraduateLeecher method of a storage.PeerStore.
func (s *PeerStore) GraduateLeecher(infoHash bittorrent.InfoHash, p bittorrent.Peer) error {
	// we can just overwrite any leecher we already have, so
	return s.PutSeeder(infoHash, p)
}

func (s *PeerStore) putPeer(ih infohash, peer *peer, af bittorrent.AddressFamily) (swarmCreated bool) {
	shard := s.shards.lockShardByHash(ih)

	pl, ok := shard.swarms[ih]
	if !ok {
		swarmCreated = true
		if af == bittorrent.IPv4 {
			pl = swarm{peers4: newPeerList()}
		} else {
			pl = swarm{peers6: newPeerList()}
		}
		shard.swarms[ih] = pl
	}

	if af == bittorrent.IPv4 {
		if pl.peers4 == nil {
			pl.peers4 = newPeerList()
			shard.swarms[ih] = pl
		}

		deltaPeers, deltaSeeders := pl.peers4.putPeer(peer)
		if deltaPeers != 0 {
			pl.peers4.rebalanceBuckets()
			shard.numPeers += deltaPeers
		}
		shard.numSeeders = uint64(int64(shard.numSeeders) + deltaSeeders)
	} else {
		if pl.peers6 == nil {
			pl.peers6 = newPeerList()
			shard.swarms[ih] = pl
		}

		deltaPeers, deltaSeeders := pl.peers6.putPeer(peer)
		if deltaPeers != 0 {
			pl.peers6.rebalanceBuckets()
			shard.numPeers += deltaPeers
		}
		shard.numSeeders = uint64(int64(shard.numSeeders) + deltaSeeders)
	}

	if swarmCreated {
		s.shards.unlockShardByHash(ih, 1)
	} else {
		s.shards.unlockShardByHash(ih, 0)
	}
	return
}

func (s *PeerStore) deletePeer(ih infohash, peer *peer, af bittorrent.AddressFamily) (deleted bool, err error) {
	shard := s.shards.lockShardByHash(ih)
	defer func() {
		if deleted {
			s.shards.unlockShardByHash(ih, -1)
		} else {
			s.shards.unlockShardByHash(ih, 0)
		}
	}()

	pl, ok := shard.swarms[ih]
	if !ok {
		return false, storage.ErrResourceDoesNotExist
	}

	if af == bittorrent.IPv4 {
		if pl.peers4 == nil {
			return false, storage.ErrResourceDoesNotExist
		}

		found, seeder := pl.peers4.removePeer(peer)
		if !found {
			return false, storage.ErrResourceDoesNotExist
		}
		shard.numPeers--
		if seeder {
			shard.numSeeders--
		}

		if pl.peers4.numPeers == 0 {
			pl.peers4 = nil
			shard.swarms[ih] = pl
		} else {
			pl.peers4.rebalanceBuckets()
		}
	} else {
		if pl.peers6 == nil {
			return false, storage.ErrResourceDoesNotExist
		}

		found, seeder := pl.peers6.removePeer(peer)
		if !found {
			return false, storage.ErrResourceDoesNotExist
		}
		shard.numPeers--
		if seeder {
			shard.numSeeders--
		}

		if pl.peers6.numPeers == 0 {
			pl.peers6 = nil
			shard.swarms[ih] = pl
		} else {
			pl.peers6.rebalanceBuckets()
		}
	}

	if (pl.peers4 == nil && pl.peers6 == nil) || (pl.peers6 == nil && pl.peers4.numPeers == 0) || (pl.peers4 == nil && pl.peers6.numPeers == 0) {
		delete(shard.swarms, ih)
		deleted = true
	}

	return
}

func deriveEntropyFromRequest(infoHash bittorrent.InfoHash, p bittorrent.Peer) (uint64, uint64) {
	v0 := binary.BigEndian.Uint64([]byte(infoHash[:8])) + binary.BigEndian.Uint64([]byte(infoHash[8:16]))
	v1 := binary.BigEndian.Uint64([]byte(p.ID[:8])) + binary.BigEndian.Uint64([]byte(p.ID[8:16]))
	return v0, v1
}

// AnnouncePeers implements the AnnouncePeers method of a storage.PeerStore.
func (s *PeerStore) AnnouncePeers(infoHash bittorrent.InfoHash, seeder bool, numWant int, announcingPeer bittorrent.Peer) ([]bittorrent.Peer, error) {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	if announcingPeer.IP.AddressFamily != bittorrent.IPv4 && announcingPeer.IP.AddressFamily != bittorrent.IPv6 {
		return nil, ErrInvalidIP
	}

	ih := infohash(infoHash)
	s0, s1 := deriveEntropyFromRequest(infoHash, announcingPeer)

	p := &peer{}
	p.setPort(announcingPeer.Port)
	p.setIP(announcingPeer.IP.To16())
	return s.announceSingleStack(ih, seeder, numWant, p, announcingPeer.IP.AddressFamily, s0, s1)
}

func (s *PeerStore) announceSingleStack(ih infohash, seeder bool, numWant int, p *peer, af bittorrent.AddressFamily, s0, s1 uint64) (peers []bittorrent.Peer, err error) {
	shard := s.shards.rLockShardByHash(ih)

	pl, ok := shard.swarms[ih]
	if !ok {
		s.shards.rUnlockShardByHash(ih)
		return nil, storage.ErrResourceDoesNotExist
	}

	var ps []peer
	if af == bittorrent.IPv4 {
		ps = pl.peers4.getAnnouncePeers(numWant, seeder, p, s0, s1)
	} else {
		ps = pl.peers6.getAnnouncePeers(numWant, seeder, p, s0, s1)
	}
	s.shards.rUnlockShardByHash(ih)

	peers = make([]bittorrent.Peer, len(ps))
	for i, p := range ps {
		if af == bittorrent.IPv4 {
			peers[i] = bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip4()), AddressFamily: bittorrent.IPv4}, Port: p.port()}
			continue
		}
		peers[i] = bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip()), AddressFamily: bittorrent.IPv6}, Port: p.port()}
	}

	return
}

// ScrapeSwarm implements the ScrapeSwarm method of a storage.PeerStore.
func (s *PeerStore) ScrapeSwarm(infoHash bittorrent.InfoHash, af bittorrent.AddressFamily) (scrape bittorrent.Scrape) {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	scrape.InfoHash = infoHash
	ih := infohash(infoHash)
	shard := s.shards.rLockShardByHash(ih)

	pl, ok := shard.swarms[ih]
	if !ok {
		s.shards.rUnlockShardByHash(ih)
		return
	}

	if af == bittorrent.IPv6 {
		if pl.peers6 != nil {
			scrape.Complete = uint32(pl.peers6.numSeeders)
			scrape.Incomplete = uint32(pl.peers6.numPeers - pl.peers6.numSeeders)
		}
	} else {
		if pl.peers4 != nil {
			scrape.Complete = uint32(pl.peers4.numSeeders)
			scrape.Incomplete = uint32(pl.peers4.numPeers - pl.peers4.numSeeders)
		}
	}

	s.shards.rUnlockShardByHash(ih)
	return
}

// NumSeeders returns the number of seeders for the given infohash.
func (s *PeerStore) NumSeeders(infoHash bittorrent.InfoHash) int {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	ih := infohash(infoHash)
	shard := s.shards.rLockShardByHash(ih)

	pl, ok := shard.swarms[ih]
	if !ok {
		s.shards.rUnlockShardByHash(ih)
		return 0
	}

	totalSeeders := 0
	if pl.peers4 != nil {
		totalSeeders += pl.peers4.numSeeders
	}
	if pl.peers6 != nil {
		totalSeeders += pl.peers6.numSeeders
	}

	s.shards.rUnlockShardByHash(ih)
	return totalSeeders
}

// NumLeechers returns the number of leechers for the given infohash.
func (s *PeerStore) NumLeechers(infoHash bittorrent.InfoHash) int {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	ih := infohash(infoHash)
	shard := s.shards.rLockShardByHash(ih)

	pl, ok := shard.swarms[ih]
	if !ok {
		s.shards.rUnlockShardByHash(ih)
		return 0
	}

	totalLeechers := 0
	if pl.peers4 != nil {
		totalLeechers += (pl.peers4.numPeers - pl.peers4.numSeeders)
	}
	if pl.peers6 != nil {
		totalLeechers += (pl.peers6.numPeers - pl.peers6.numSeeders)
	}

	s.shards.rUnlockShardByHash(ih)
	return totalLeechers
}

// GetSeeders returns all seeders for the given infohash.
func (s *PeerStore) GetSeeders(infoHash bittorrent.InfoHash) (peers4, peers6 []bittorrent.Peer, err error) {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	ih := infohash(infoHash)
	shard := s.shards.rLockShardByHash(ih)

	pl, ok := shard.swarms[ih]
	if !ok {
		s.shards.rUnlockShardByHash(ih)
		return nil, nil, storage.ErrResourceDoesNotExist
	}

	var ps4, ps6 []peer
	if pl.peers4 != nil {
		ps4 = pl.peers4.getAllSeeders()
	}
	if pl.peers6 != nil {
		ps6 = pl.peers6.getAllSeeders()
	}
	s.shards.rUnlockShardByHash(ih)

	for _, p := range ps4 {
		peers4 = append(peers4, bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip4()), AddressFamily: bittorrent.IPv4}, Port: p.port()})
	}

	for _, p := range ps6 {
		peers6 = append(peers6, bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip()), AddressFamily: bittorrent.IPv6}, Port: p.port()})
	}

	return
}

// GetLeechers returns all leechers for the given infohash.
func (s *PeerStore) GetLeechers(infoHash bittorrent.InfoHash) (peers4, peers6 []bittorrent.Peer, err error) {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	ih := infohash(infoHash)
	shard := s.shards.rLockShardByHash(ih)

	pl, ok := shard.swarms[ih]
	if !ok {
		s.shards.rUnlockShardByHash(ih)
		return nil, nil, storage.ErrResourceDoesNotExist
	}

	var ps4, ps6 []peer
	if pl.peers4 != nil {
		ps4 = pl.peers4.getAllLeechers()
	}
	if pl.peers6 != nil {
		ps6 = pl.peers6.getAllLeechers()
	}
	s.shards.rUnlockShardByHash(ih)

	for _, p := range ps4 {
		peers4 = append(peers4, bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip4()), AddressFamily: bittorrent.IPv4}, Port: p.port()})
	}

	for _, p := range ps6 {
		peers6 = append(peers6, bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip()), AddressFamily: bittorrent.IPv6}, Port: p.port()})
	}

	return
}

// Stop implements the Stop method of a storage.PeerStore.
func (s *PeerStore) Stop() <-chan error {
	select {
	case <-s.closed:
		return stop.AlreadyStopped
	default:
	}
	toReturn := make(chan error)
	go func() {
		close(s.closed)
		s.wg.Wait()

		s.shards = newShardContainer(s.cfg.ShardCountBits)
		close(toReturn)
	}()
	return toReturn
}

// NumSwarms returns the total number of swarms tracked by the PeerStore.
// This is the same as the amount of infohashes tracked.
// Runs in constant time, is exactly accurate.
func (s *PeerStore) NumSwarms() uint64 {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	return s.shards.getTorrentCount()
}

// NumTotalPeers returns the total number of peers tracked by the PeerStore.
// Runs in linear time in regards to the number of swarms tracked. The numbers
// returned are approximate.
func (s *PeerStore) NumTotalPeers() (seeders, leechers uint64) {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	for i := 0; i < len(s.shards.shards); i++ {
		shard := s.shards.rLockShard(i)
		seeders += shard.numSeeders
		leechers += shard.numPeers - shard.numSeeders
		s.shards.rUnlockShard(i)
	}

	return seeders, leechers
}
