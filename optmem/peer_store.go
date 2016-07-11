package optmem

import (
	"bytes"
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

	gcInterval, err := time.ParseDuration(cfg.GCInterval)
	if err != nil {
		return nil, err
	}

	gcCutoff, err := time.ParseDuration(cfg.GCCutoff)
	if err != nil {
		return nil, err
	}

	ps := &peerStore{
		shards: newShardContainer(cfg.ShardCountBits),
		closed: make(chan struct{}),
		cfg:    cfg,
	}

	go func() {
		for {
			select {
			case <-ps.closed:
				return
			case <-time.After(gcInterval):
				cutoffTime := time.Now().Add(gcCutoff * -1)
				logln("collecting garbage. Cutoff time: " + cutoffTime.String())
				err := ps.CollectGarbage(cutoffTime)
				if err != nil {
					logln("failed to collect garbage: " + err.Error())
				} else {
					logln("finished collecting garbage")
				}
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
	logf("running GC. internal cutoff: %d, maxDiff: %d\n", internalCutoff, maxDiff)

	for i := 0; i < len(s.shards.shards); i++ {
		deltaTorrents := 0
		shard := s.shards.lockShard(i)

		for ih, s := range shard.swarms {
			gc := s.peers.collectGarbage(internalCutoff, maxDiff)
			if gc {
				s.peers.rebalanceBuckets()
			}
			if s.peers.numPeers == 0 {
				delete(shard.swarms, ih)
				deltaTorrents--
			}
		}

		s.shards.unlockShard(i, deltaTorrents)
		runtime.Gosched()
	}
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

	peer := &peer{}
	v6 := p.IP.To16()
	if v6 == nil {
		return ErrInvalidIP
	}
	peer.setIP(v6)
	peer.setPort(p.Port)
	peer.setPeerFlag(peerFlagSeeder)
	peer.setPeerTime(uint16(time.Now().Unix()))
	var created bool
	ih := infohash(infoHash)

	shard := s.shards.lockShardByHash(ih)
	defer func() {
		if created {
			s.shards.unlockShardByHash(ih, 1)
		} else {
			s.shards.unlockShardByHash(ih, 0)
		}
	}()

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		created = true
		pl = swarm{peers: newPeerList()}
		shard.swarms[ih] = pl
	}

	pl.peers.putPeer(peer)
	pl.peers.rebalanceBuckets()

	return nil
}

func (s *peerStore) DeleteSeeder(infoHash chihaya.InfoHash, p chihaya.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	peer := &peer{}
	v6 := p.IP.To16()
	if v6 == nil {
		return ErrInvalidIP
	}
	peer.setIP(v6)
	peer.setPort(p.Port)
	peer.setPeerFlag(peerFlagSeeder)
	ih := infohash(infoHash)

	shard := s.shards.lockShardByHash(ih)
	defer s.shards.unlockShardByHash(ih, 0)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		return store.ErrResourceDoesNotExist
	}
	found := pl.peers.removePeer(peer)
	if !found {
		return store.ErrResourceDoesNotExist
	}

	if pl.peers.numPeers == 0 {
		delete(shard.swarms, ih)
		return nil
	}

	pl.peers.rebalanceBuckets()

	return nil
}

func (s *peerStore) PutLeecher(infoHash chihaya.InfoHash, p chihaya.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	peer := &peer{}
	v6 := p.IP.To16()
	if v6 == nil {
		return ErrInvalidIP
	}
	peer.setIP(v6)
	peer.setPort(p.Port)
	peer.setPeerFlag(peerFlagLeecher)
	peer.setPeerTime(uint16(time.Now().Unix()))
	var created bool
	ih := infohash(infoHash)

	shard := s.shards.lockShardByHash(ih)
	defer func() {
		if created {
			s.shards.unlockShardByHash(ih, 1)
		} else {
			s.shards.unlockShardByHash(ih, 0)
		}
	}()

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		created = true
		pl = swarm{peers: newPeerList()}
		shard.swarms[ih] = pl
	}

	pl.peers.putPeer(peer)
	pl.peers.rebalanceBuckets()

	return nil
}

func (s *peerStore) DeleteLeecher(infoHash chihaya.InfoHash, p chihaya.Peer) error {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	peer := &peer{}
	v6 := p.IP.To16()
	if v6 == nil {
		return ErrInvalidIP
	}
	peer.setIP(v6)
	peer.setPort(p.Port)
	peer.setPeerFlag(peerFlagLeecher)
	ih := infohash(infoHash)

	shard := s.shards.lockShardByHash(ih)
	defer s.shards.unlockShardByHash(ih, 0)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		return store.ErrResourceDoesNotExist
	}
	found := pl.peers.removePeer(peer)
	if !found {
		return store.ErrResourceDoesNotExist
	}

	if pl.peers.numPeers == 0 {
		delete(shard.swarms, ih)
		return nil
	}

	pl.peers.rebalanceBuckets()

	return nil
}

func (s *peerStore) GraduateLeecher(infoHash chihaya.InfoHash, p chihaya.Peer) error {
	// we can just overwrite any leecher we already have, so
	return s.PutSeeder(infoHash, p)
}

func (s *peerStore) AnnouncePeers(infoHash chihaya.InfoHash, seeder bool, numWant int, peer4, peer6 chihaya.Peer) (peers4, peers6 []chihaya.Peer, err error) {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	peer := &peer{}
	if peer4.IP != nil && (len(peer4.IP) == 4 || len(peer4.IP) == 16) {
		peer.setIP(peer4.IP.To16())
		peer.setPort(peer4.Port)
	} else if peer6.IP != nil && len(peer6.IP) == 16 {
		peer.setIP(peer6.IP.To16())
		peer.setPort(peer6.Port)
	} else {
		return nil, nil, ErrInvalidIP
	}
	ih := infohash(infoHash)

	shard := s.shards.lockShardByHash(ih)
	defer s.shards.unlockShardByHash(ih, 0)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		return nil, nil, store.ErrResourceDoesNotExist
	}

	peers := pl.peers.getAnnouncePeers(numWant, seeder, peer)

	for _, p := range peers {
		if bytes.Equal(p.data[:12], v4InV6Prefix) {
			peers4 = append(peers4, chihaya.Peer{IP: net.IP(p.ip()), Port: p.port()})
		} else {
			peers6 = append(peers6, chihaya.Peer{IP: net.IP(p.ip()), Port: p.port()})
		}
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

	shard := s.shards.lockShardByHash(ih)
	defer s.shards.unlockShardByHash(ih, 0)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		return 0
	}
	return pl.peers.numSeeders
}

func (s *peerStore) NumLeechers(infoHash chihaya.InfoHash) int {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	ih := infohash(infoHash)

	shard := s.shards.lockShardByHash(ih)
	defer s.shards.unlockShardByHash(ih, 0)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		return 0
	}
	return pl.peers.numPeers - pl.peers.numSeeders
}

var v4InV6Prefix = []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff}

func (s *peerStore) GetSeeders(infoHash chihaya.InfoHash) (peers4, peers6 []chihaya.Peer, err error) {
	select {
	case <-s.closed:
		panic("attempted to interact with closed store")
	default:
	}

	ih := infohash(infoHash)

	shard := s.shards.lockShardByHash(ih)
	defer s.shards.unlockShardByHash(ih, 0)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		return nil, nil, store.ErrResourceDoesNotExist
	}

	peers := pl.peers.getAllSeeders()

	for _, p := range peers {
		if bytes.Equal(p.data[:12], v4InV6Prefix) {
			peers4 = append(peers4, chihaya.Peer{IP: net.IP(p.ip()), Port: p.port()})
		} else {
			peers6 = append(peers6, chihaya.Peer{IP: net.IP(p.ip()), Port: p.port()})
		}
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

	shard := s.shards.lockShardByHash(ih)
	defer s.shards.unlockShardByHash(ih, 0)

	var pl swarm
	var ok bool
	if pl, ok = shard.swarms[ih]; !ok {
		return nil, nil, store.ErrResourceDoesNotExist
	}

	peers := pl.peers.getAllLeechers()

	for _, p := range peers {
		if bytes.Equal(p.data[:12], v4InV6Prefix) {
			peers4 = append(peers4, chihaya.Peer{IP: net.IP(p.ip()), Port: p.port()})
		} else {
			peers6 = append(peers6, chihaya.Peer{IP: net.IP(p.ip()), Port: p.port()})
		}
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
		s.shards = newShardContainer(s.cfg.ShardCountBits)
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
		shard := s.shards.lockShard(i)
		for _, s := range shard.swarms {
			n += uint64(s.peers.numSeeders)
		}
		s.shards.unlockShard(i, 0)
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
		shard := s.shards.lockShard(i)
		for _, s := range shard.swarms {
			n += uint64(s.peers.numPeers - s.peers.numSeeders)
		}
		s.shards.unlockShard(i, 0)
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
		shard := s.shards.lockShard(i)
		for _, s := range shard.swarms {
			n += uint64(s.peers.numPeers)
		}
		s.shards.unlockShard(i, 0)
		runtime.Gosched()
	}

	return n
}
