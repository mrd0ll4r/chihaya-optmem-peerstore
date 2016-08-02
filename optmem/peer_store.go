package optmem

import (
	"errors"
	"net"
	"runtime"
	"time"

	"github.com/chihaya/chihaya"
	"github.com/chihaya/chihaya/pkg/stopper"
	"github.com/chihaya/chihaya/server/store"
)

func init() {
	store.RegisterPeerStoreDriver("optmem", &peerStoreDriver{})
}

// ErrInvalidIP is returned if a peer with an invalid IP was specified.
var ErrInvalidIP = errors.New("invalid IP")

type peerStoreDriver struct{}

func (p *peerStoreDriver) New(storecfg *store.DriverConfig) (store.PeerStore, error) {
	cfg, err := newPeerStoreConfig(storecfg)
	if err != nil {
		return nil, err
	}

	ps := &peerStore{
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

type peerStore struct {
	shards *shardContainer
	closed chan struct{}
	cfg    *peerStoreConfig
}

func (s *peerStore) collectGarbage(cutoff time.Time) {
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

func (s *peerStore) CollectGarbage(cutoff time.Time) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	s.collectGarbage(cutoff)
	return nil
}

func (s *peerStore) PutSeeder(infoHash chihaya.InfoHash, p chihaya.Peer) error {
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

func (s *peerStore) DeleteSeeder(infoHash chihaya.InfoHash, p chihaya.Peer) error {
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

	s.deletePeer(ih, peer, pType)

	return nil
}

func (s *peerStore) PutLeecher(infoHash chihaya.InfoHash, p chihaya.Peer) error {
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

func (s *peerStore) DeleteLeecher(infoHash chihaya.InfoHash, p chihaya.Peer) error {
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

	s.deletePeer(ih, peer, pType)

	return nil
}

func (s *peerStore) GraduateLeecher(infoHash chihaya.InfoHash, p chihaya.Peer) error {
	// we can just overwrite any leecher we already have, so
	return s.PutSeeder(infoHash, p)
}

func (s *peerStore) putPeer(ih infohash, peer *peer, pType peerType) (created bool) {
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

func (s *peerStore) deletePeer(ih infohash, peer *peer, pType peerType) (deleted bool, err error) {
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
		return false, store.ErrResourceDoesNotExist
	}

	if pType == v4Peer {
		if pl.peers4 == nil {
			return false, store.ErrResourceDoesNotExist
		}

		found := pl.peers4.removePeer(peer)
		if !found {
			return false, store.ErrResourceDoesNotExist
		}

		if pl.peers4.numPeers == 0 {
			pl.peers4 = nil
			shard.swarms[ih] = pl
		} else {
			pl.peers4.rebalanceBuckets()
		}
	} else {
		if pl.peers6 == nil {
			return false, store.ErrResourceDoesNotExist
		}

		found := pl.peers6.removePeer(peer)
		if !found {
			return false, store.ErrResourceDoesNotExist
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

func (s *peerStore) AnnouncePeers(infoHash chihaya.InfoHash, seeder bool, numWant int, peer4, peer6 chihaya.Peer) (peers4, peers6 []chihaya.Peer, err error) {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	ih := infohash(infoHash)

	var p4, p6 *peer
	if peer4.IP != nil && (len(peer4.IP) == 4 || len(peer4.IP) == 16) {
		p4 = &peer{}
		p4.setIP(peer4.IP.To16())
		p4.setPort(peer4.Port)
	}
	if peer6.IP != nil && len(peer6.IP) == 16 {
		p6 = &peer{}
		p6.setIP(peer6.IP)
		p6.setPort(peer6.Port)
	}

	if p4 == nil && p6 == nil {
		return nil, nil, ErrInvalidIP
	}

	if p4 != nil {
		peers4, err = s.announceSingleStack(ih, seeder, numWant, p4, v4Peer)
		if err != nil {
			return nil, nil, err
		}
	}

	if p6 != nil {
		peers6, err = s.announceSingleStack(ih, seeder, numWant, p6, v6Peer)
		if err != nil {
			if peers4 != nil && len(peers4) != 0 {
				return peers4, nil, nil
			}
			return nil, nil, err
		}
	}

	return
}

func (s *peerStore) announceSingleStack(ih infohash, seeder bool, numWant int, p *peer, pType peerType) (peers []chihaya.Peer, err error) {
	shard := s.shards.rLockShardByHash(ih)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		s.shards.rUnlockShardByHash(ih)
		return nil, store.ErrResourceDoesNotExist
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
			peers = append(peers, chihaya.Peer{IP: net.IP(p.ip()).To4(), Port: p.port()})
			continue
		}
		peers = append(peers, chihaya.Peer{IP: net.IP(p.ip()), Port: p.port()})
	}

	return
}

func (s *peerStore) NumSeeders(infoHash chihaya.InfoHash) int {
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

func (s *peerStore) NumLeechers(infoHash chihaya.InfoHash) int {
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

func (s *peerStore) GetSeeders(infoHash chihaya.InfoHash) (peers4, peers6 []chihaya.Peer, err error) {
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
		return nil, nil, store.ErrResourceDoesNotExist
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
		peers4 = append(peers4, chihaya.Peer{IP: net.IP(p.ip()).To4(), Port: p.port()})
	}

	for _, p := range ps6 {
		peers6 = append(peers6, chihaya.Peer{IP: net.IP(p.ip()), Port: p.port()})
	}

	return
}

func (s *peerStore) GetLeechers(infoHash chihaya.InfoHash) (peers4, peers6 []chihaya.Peer, err error) {
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
		return nil, nil, store.ErrResourceDoesNotExist
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
		peers4 = append(peers4, chihaya.Peer{IP: net.IP(p.ip()).To4(), Port: p.port()})
	}

	for _, p := range ps6 {
		peers6 = append(peers6, chihaya.Peer{IP: net.IP(p.ip()), Port: p.port()})
	}

	return
}

func (s *peerStore) Stop() <-chan error {
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
func (s *peerStore) NumSwarms() uint64 {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	return s.shards.getTorrentCount()
}

// NumTotalSeeders returns the total number of seeders tracked by the PeerStore.
// Runs in linear time in regards to the number of swarms tracked.
func (s *peerStore) NumTotalSeeders() uint64 {
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
func (s *peerStore) NumTotalLeechers() uint64 {
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
func (s *peerStore) NumTotalPeers() uint64 {
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
