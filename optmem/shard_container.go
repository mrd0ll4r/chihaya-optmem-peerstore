package optmem

import (
	"encoding/binary"
	"sync"
)

type shardContainer struct {
	shards          []shard
	numTorrents     uint64
	shardCountShift uint
	shardLocks      []*sync.Mutex // mutexes for the shards
	m               *sync.Mutex   // mutex for numTorrents
}

func newShardContainer(shardCountBits uint) *shardContainer {
	shardCount := 1 << shardCountBits      // this is the amount of shards of the infohash keyspace we have
	shardCountShift := 32 - shardCountBits // we need this to quickly find the shard for an infohash

	toReturn := shardContainer{
		shards:          make([]shard, shardCount),
		shardCountShift: shardCountShift,
		m:               &sync.Mutex{},
		shardLocks:      make([]*sync.Mutex, shardCount),
	}
	for i := 0; i < shardCount; i++ {
		toReturn.shards[i] = shard{
			swarms: make(map[infohash]swarm),
		}
		toReturn.shardLocks[i] = &sync.Mutex{}
	}
	return &toReturn
}

func (s *shardContainer) lockShard(shard int) *shard {
	s.shardLocks[shard].Lock()
	return &s.shards[shard]
}

func (s *shardContainer) lockShardByHash(hash infohash) *shard {
	u := binary.BigEndian.Uint32(hash[:8])
	return s.lockShard(int(u >> s.shardCountShift))
}

func (s *shardContainer) unlockShard(shard, numTorrentsDelta int) {
	s.shardLocks[shard].Unlock()
	s.m.Lock()
	defer s.m.Unlock()
	s.numTorrents += uint64(numTorrentsDelta)
}

func (s *shardContainer) unlockShardByHash(hash infohash, numTorrentsDelta int) {
	u := binary.BigEndian.Uint32(hash[:8])
	s.unlockShard(int(u>>s.shardCountShift), numTorrentsDelta)
}

func (s *shardContainer) getTorrentCount() uint64 {
	s.m.Lock()
	defer s.m.Unlock()
	return s.numTorrents
}
