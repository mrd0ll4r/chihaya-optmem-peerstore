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
		shard := s.shards.lockShard(i)

		for ih, s := range shard.swarms {
			if s.peers4 != nil {
				gc := s.peers4.collectGarbage(internalCutoff, maxDiff)
				if gc {
					s.peers4.rebalanceBuckets()
				}
				if s.peers4.numPeers == 0 {
					s.peers4 = nil
					shard.swarms[ih] = s
				}
			}

			if s.peers6 != nil {
				gc := s.peers6.collectGarbage(internalCutoff, maxDiff)
				if gc {
					s.peers6.rebalanceBuckets()
				}
				if s.peers6.numPeers == 0 {
					s.peers6 = nil
					shard.swarms[ih] = s
				}
			}

			if s.peers4 == nil && s.peers6 == nil {
				delete(shard.swarms, ih)
				deltaTorrents--
			}
		}

		s.shards.unlockShard(i, deltaTorrents)
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

	peer := makePeer(p, peerFlagSeeder, uint16(time.Now().Unix()))
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

	peer := makePeer(p, peerFlagLeecher, uint16(time.Now().Unix()))
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

func (s *PeerStore) putPeer(ih infohash, peer *peer, af bittorrent.AddressFamily) (created bool) {
	shard := s.shards.lockShardByHash(ih)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		created = true
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

		pl.peers4.putPeer(peer)
		pl.peers4.rebalanceBuckets()
	} else {
		if pl.peers6 == nil {
			pl.peers6 = newPeerList()
			shard.swarms[ih] = pl
		}

		pl.peers6.putPeer(peer)
		pl.peers6.rebalanceBuckets()
	}

	if created {
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

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		return false, storage.ErrResourceDoesNotExist
	}

	if af == bittorrent.IPv4 {
		if pl.peers4 == nil {
			return false, storage.ErrResourceDoesNotExist
		}

		found := pl.peers4.removePeer(peer)
		if !found {
			return false, storage.ErrResourceDoesNotExist
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

		found := pl.peers6.removePeer(peer)
		if !found {
			return false, storage.ErrResourceDoesNotExist
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

	ih := infohash(infoHash)

	// TODO maybe remove?
	if len(announcingPeer.IP.IP) != net.IPv4len && len(announcingPeer.IP.IP) != net.IPv6len {
		return nil, ErrInvalidIP
	}
	s0, s1 := deriveEntropyFromRequest(infoHash, announcingPeer)

	p := &peer{}
	p.setPort(announcingPeer.Port)
	switch announcingPeer.IP.AddressFamily {
	case bittorrent.IPv4:
		p.setIP(announcingPeer.IP.To16())
		return s.announceSingleStack(ih, seeder, numWant, p, bittorrent.IPv4, s0, s1)
	case bittorrent.IPv6:
		p.setIP(announcingPeer.IP.To16())
		return s.announceSingleStack(ih, seeder, numWant, p, bittorrent.IPv6, s0, s1)
	default:
		panic("peer was neither v4 nor v6")
	}
}

func (s *PeerStore) announceSingleStack(ih infohash, seeder bool, numWant int, p *peer, af bittorrent.AddressFamily, s0, s1 uint64) (peers []bittorrent.Peer, err error) {
	shard := s.shards.rLockShardByHash(ih)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
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

	for _, p := range ps {
		if af == bittorrent.IPv4 {
			peers = append(peers, bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip()).To4(), AddressFamily: bittorrent.IPv4}, Port: p.port()})
			continue
		}
		peers = append(peers, bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip()), AddressFamily: bittorrent.IPv6}, Port: p.port()})
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

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
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

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
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

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
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
		peers4 = append(peers4, bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip()).To4(), AddressFamily: bittorrent.IPv4}, Port: p.port()})
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

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
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
		peers4 = append(peers4, bittorrent.Peer{IP: bittorrent.IP{IP: net.IP(p.ip()).To4(), AddressFamily: bittorrent.IPv4}, Port: p.port()})
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
		s.shards = newShardContainer(s.cfg.ShardCountBits)
		close(s.closed)
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
		for _, s := range shard.swarms {
			if s.peers4 != nil {
				leechers += uint64(s.peers4.numPeers - s.peers4.numSeeders)
				seeders += uint64(s.peers4.numSeeders)
			}
			if s.peers6 != nil {
				leechers += uint64(s.peers6.numPeers - s.peers6.numSeeders)
				seeders += uint64(s.peers6.numSeeders)
			}
		}
		s.shards.rUnlockShard(i)
		runtime.Gosched()
	}

	return seeders, leechers
}
