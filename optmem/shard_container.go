package optmem

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
)

type shardContainer struct {
	shards          []*shard
	numTorrents     *uint64
	shardCountShift uint
	shardLocks      []*sync.RWMutex // mutexes for the shards
}

func newShardContainer(shardCountBits uint) *shardContainer {
	shardCount := 1 << shardCountBits      // this is the amount of shards of the infohash keyspace we have
	shardCountShift := 32 - shardCountBits // we need this to quickly find the shard for an infohash
	numTorrents := uint64(0)

	toReturn := shardContainer{
		shards:          make([]*shard, shardCount),
		shardCountShift: shardCountShift,
		shardLocks:      make([]*sync.RWMutex, shardCount),
		numTorrents:     &numTorrents,
	}
	for i := 0; i < shardCount; i++ {
		toReturn.shards[i] = &shard{
			swarms: make(map[infohash]swarm),
		}
		toReturn.shardLocks[i] = &sync.RWMutex{}
	}
	return &toReturn
}

func (s *shardContainer) rLockShard(shard int) *shard {
	s.shardLocks[shard].RLock()
	return s.shards[shard]
}

func (s *shardContainer) rLockShardByHash(hash infohash) *shard {
	u := int(binary.BigEndian.Uint32(hash[:8]) >> s.shardCountShift)
	return s.rLockShard(u)
}

func (s *shardContainer) rUnlockShard(shard int) {
	s.shardLocks[shard].RUnlock()
}

func (s *shardContainer) rUnlockShardByHash(hash infohash) {
	u := int(binary.BigEndian.Uint32(hash[:8]) >> s.shardCountShift)
	s.rUnlockShard(u)
}

func (s *shardContainer) lockShard(shard int) *shard {
	s.shardLocks[shard].Lock()
	return s.shards[shard]
}

func (s *shardContainer) lockShardByHash(hash infohash) *shard {
	u := int(binary.BigEndian.Uint32(hash[:8]) >> s.shardCountShift)
	s.shardLocks[u].Lock()
	return s.shards[u]
}

func (s *shardContainer) unlockShard(shard, numTorrentsDelta int) {
	s.shardLocks[shard].Unlock()
	atomic.AddUint64(s.numTorrents, uint64(numTorrentsDelta))
}

func (s *shardContainer) unlockShardByHash(hash infohash, numTorrentsDelta int) {
	u := int(binary.BigEndian.Uint32(hash[:8]) >> s.shardCountShift)
	s.unlockShard(u, numTorrentsDelta)
}

func (s *shardContainer) getTorrentCount() uint64 {
	return atomic.LoadUint64(s.numTorrents)
}
