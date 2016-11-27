package optmem

import (
	"net"
	"runtime"
	"time"

	"github.com/chihaya/chihaya/bittorrent"
	"github.com/chihaya/chihaya/stopper"
	"github.com/chihaya/chihaya/storage"
	"github.com/pkg/errors"
)

// ErrInvalidIP is returned if a peer with an invalid IP was specified.
var ErrInvalidIP = errors.New("invalid IP")

var _ storage.PeerStore = &PeerStore{}

// New creates a new PeerStore from the config.
func New(cfg Config) (*PeerStore, error) {
	cfg, err := validateConfig(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "invalid config")
	}

	ps := &PeerStore{
		shards: newShardContainer(cfg.ShardCountBits, cfg.RandomParallelism),
		closed: make(chan struct{}),
		cfg:    cfg,
	}

	go func() {
		for {
			select {
			case <-ps.closed:
				return
			case <-time.After(cfg.GCInterval):
				cutoffTime := time.Now().Add(cfg.GCCutoff * -1)
				logln("collecting garbage. Cutoff time: " + cutoffTime.String())
				ps.collectGarbage(cutoffTime)
				logln("finished collecting garbage")
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
}

func (s *PeerStore) collectGarbage(cutoff time.Time) {
	internalCutoff := uint16(cutoff.Unix())
	maxDiff := uint16(time.Now().Unix() - cutoff.Unix())
	logf("running GC. internal cutoff: %d, maxDiff: %d, infohashes: %d, peers: %d\n", internalCutoff, maxDiff, s.NumSwarms(), s.NumTotalPeers())

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

	logf("GC done. infohashes: %d, peers: %d\n", s.NumSwarms(), s.NumTotalPeers())
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

	pType := determinePeerType(p)
	if pType == invalidPeer {
		return ErrInvalidIP
	}

	peer := makePeer(p, peerFlagSeeder, uint16(time.Now().Unix()))
	ih := infohash(infoHash)

	s.putPeer(ih, peer, pType)

	return nil
}

// DeleteSeeder implements the DeleteSeeder method of a storage.PeerStore.
func (s *PeerStore) DeleteSeeder(infoHash bittorrent.InfoHash, p bittorrent.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	pType := determinePeerType(p)
	if pType == invalidPeer {
		return ErrInvalidIP
	}

	peer := makePeer(p, peerFlagSeeder, uint16(0))
	ih := infohash(infoHash)

	_, err := s.deletePeer(ih, peer, pType)

	return err
}

// PutLeecher implements the PutLeecher method of a storage.PeerStore.
func (s *PeerStore) PutLeecher(infoHash bittorrent.InfoHash, p bittorrent.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	pType := determinePeerType(p)
	if pType == invalidPeer {
		return ErrInvalidIP
	}

	peer := makePeer(p, peerFlagLeecher, uint16(time.Now().Unix()))
	ih := infohash(infoHash)

	s.putPeer(ih, peer, pType)

	return nil
}

// DeleteLeecher implements the DeleteLeecher method of a storage.PeerStore.
func (s *PeerStore) DeleteLeecher(infoHash bittorrent.InfoHash, p bittorrent.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	pType := determinePeerType(p)
	if pType == invalidPeer {
		return ErrInvalidIP
	}

	peer := makePeer(p, peerFlagLeecher, uint16(0))
	ih := infohash(infoHash)

	_, err := s.deletePeer(ih, peer, pType)

	return err
}

// GraduateLeecher implements the GraduateLeecher method of a storage.PeerStore.
func (s *PeerStore) GraduateLeecher(infoHash bittorrent.InfoHash, p bittorrent.Peer) error {
	// we can just overwrite any leecher we already have, so
	return s.PutSeeder(infoHash, p)
}

func (s *PeerStore) putPeer(ih infohash, peer *peer, pType peerType) (created bool) {
	shard := s.shards.lockShardByHash(ih)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		created = true
		if pType == v4Peer {
			pl = swarm{peers4: newPeerList()}
		} else {
			pl = swarm{peers6: newPeerList()}
		}
		shard.swarms[ih] = pl
	}

	if pType == v4Peer {
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

func (s *PeerStore) deletePeer(ih infohash, peer *peer, pType peerType) (deleted bool, err error) {
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

	if pType == v4Peer {
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

// AnnouncePeers implements the AnnouncePeers method of a storage.PeerStore.
func (s *PeerStore) AnnouncePeers(infoHash bittorrent.InfoHash, seeder bool, numWant int, announcingPeer bittorrent.Peer) ([]bittorrent.Peer, error) {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	ih := infohash(infoHash)

	if len(announcingPeer.IP) != net.IPv4len && len(announcingPeer.IP) != net.IPv6len {
		return nil, ErrInvalidIP
	}

	p := &peer{}
	p.setPort(announcingPeer.Port)
	switch {
	case announcingPeer.IP.To4() != nil:
		p.setIP(announcingPeer.IP.To16())
		return s.announceSingleStack(ih, seeder, numWant, p, v4Peer)
	case announcingPeer.IP.To16() != nil:
		p.setIP(announcingPeer.IP.To16())
		return s.announceSingleStack(ih, seeder, numWant, p, v6Peer)
	default:
		panic("peer was neither v4 nor v6 even after we checked")
	}
}

func (s *PeerStore) announceSingleStack(ih infohash, seeder bool, numWant int, p *peer, pType peerType) (peers []bittorrent.Peer, err error) {
	shard := s.shards.rLockShardByHash(ih)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		s.shards.rUnlockShardByHash(ih)
		return nil, storage.ErrResourceDoesNotExist
	}

	var ps []peer
	r := shard.r.Get()
	if pType == v4Peer {
		ps = pl.peers4.getAnnouncePeers(numWant, seeder, p, r)
	} else {
		ps = pl.peers6.getAnnouncePeers(numWant, seeder, p, r)
	}
	shard.r.Put(r)
	s.shards.rUnlockShardByHash(ih)

	for _, p := range ps {
		if pType == v4Peer {
			peers = append(peers, bittorrent.Peer{IP: net.IP(p.ip()).To4(), Port: p.port()})
			continue
		}
		peers = append(peers, bittorrent.Peer{IP: net.IP(p.ip()), Port: p.port()})
	}

	return
}

// ScrapeSwarm implements the ScrapeSwarm method of a storage.PeerStore.
func (s *PeerStore) ScrapeSwarm(infoHash bittorrent.InfoHash, v6 bool) (scrape bittorrent.Scrape) {
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
		return
	}

	if v6 {
		if pl.peers6 != nil {
			scrape.Complete = uint32(pl.peers6.numSeeders)
			scrape.Incomplete = uint32(pl.peers6.numPeers - pl.peers6.numSeeders)
		}
		s.shards.rUnlockShardByHash(ih)
		return
	}

	// v4
	if pl.peers4 != nil {
		scrape.Complete = uint32(pl.peers4.numSeeders)
		scrape.Incomplete = uint32(pl.peers4.numPeers - pl.peers4.numSeeders)
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

var v4InV6Prefix = []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff}

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
		peers4 = append(peers4, bittorrent.Peer{IP: net.IP(p.ip()).To4(), Port: p.port()})
	}

	for _, p := range ps6 {
		peers6 = append(peers6, bittorrent.Peer{IP: net.IP(p.ip()), Port: p.port()})
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
		peers4 = append(peers4, bittorrent.Peer{IP: net.IP(p.ip()).To4(), Port: p.port()})
	}

	for _, p := range ps6 {
		peers6 = append(peers6, bittorrent.Peer{IP: net.IP(p.ip()), Port: p.port()})
	}

	return
}

// Stop implements the Stop method of a storage.PeerStore.
func (s *PeerStore) Stop() <-chan error {
	select {
	case <-s.closed:
		return stopper.AlreadyStopped
	default:
	}
	toReturn := make(chan error)
	go func() {
		s.shards = newShardContainer(s.cfg.ShardCountBits, s.cfg.RandomParallelism)
		close(s.closed)
		close(toReturn)
	}()
	return toReturn
}

// NumSwarms returns the total number of swarms tracked by the PeerStore.
// This is the same as the amount of infohashes tracked.
// Runs in constant time.
func (s *PeerStore) NumSwarms() uint64 {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	return s.shards.getTorrentCount()
}

// NumTotalSeeders returns the total number of seeders tracked by the PeerStore.
// Runs in linear time in regards to the number of swarms tracked.
func (s *PeerStore) NumTotalSeeders() uint64 {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	n := uint64(0)

	for i := 0; i < len(s.shards.shards); i++ {
		shard := s.shards.rLockShard(i)
		for _, s := range shard.swarms {
			if s.peers4 != nil {
				n += uint64(s.peers4.numSeeders)
			}
			if s.peers6 != nil {
				n += uint64(s.peers6.numSeeders)
			}
		}
		s.shards.rUnlockShard(i)
		runtime.Gosched()
	}

	return n
}

// NumTotalLeechers returns the total number of leechers tracked by the
// PeerStore.
// Runs in linear time in regards to the number of swarms tracked.
func (s *PeerStore) NumTotalLeechers() uint64 {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	n := uint64(0)

	for i := 0; i < len(s.shards.shards); i++ {
		shard := s.shards.rLockShard(i)
		for _, s := range shard.swarms {
			if s.peers4 != nil {
				n += uint64(s.peers4.numPeers - s.peers4.numSeeders)
			}
			if s.peers6 != nil {
				n += uint64(s.peers4.numPeers - s.peers6.numSeeders)
			}
		}
		s.shards.rUnlockShard(i)
		runtime.Gosched()
	}

	return n
}

// NumTotalPeers returns the total number of peers tracked by the PeerStore.
// Runs in linear time in regards to the number of swarms tracked.
// Call this instead of calling NumTotalLeechers + NumTotalSeeders.
func (s *PeerStore) NumTotalPeers() uint64 {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	n := uint64(0)

	for i := 0; i < len(s.shards.shards); i++ {
		shard := s.shards.rLockShard(i)
		for _, s := range shard.swarms {
			if s.peers4 != nil {
				n += uint64(s.peers4.numPeers)
			}
			if s.peers6 != nil {
				n += uint64(s.peers6.numPeers)
			}
		}
		s.shards.rUnlockShard(i)
		runtime.Gosched()
	}

	return n
}
