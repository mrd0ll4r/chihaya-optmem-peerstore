package optmem

import (
	"errors"
	"time"
)

var (
	// ErrInvalidGCCutoff is returned for a config with an invalid
	// gc_cutoff.
	ErrInvalidGCCutoff = errors.New("invalid gc_cutoff")

	// ErrInvalidGCInterval is returned for a config with an invalid
	// gc_interval.
	ErrInvalidGCInterval = errors.New("invalid gc_interval")
)

// Config holds the configuration for an optmen PeerStore.
type Config struct {
	// ShardCountBits specifies the number of bits to use for shard
	// indexing.
	// For example:
	// ShardCountBits = 1, shards = 2
	// ShardCountBits = 2, shards = 4
	// ShardCountBits = 3, shards = 8
	// ShardCountBits = 10, shards = 1024
	//
	// Every shard contains an equal part of all possible infohashes.
	// Increasing the number of shards will increase the base memory
	// usage, but will also decrease lock-contention, as each shard can
	// be locked independently.
	//
	// Having shards >= 1024 is recommended unless you really know what you
	// are doing.
	ShardCountBits uint `yaml:"shard_count_bits"`

	// RandomParallelism specifies how many random sources to make available
	// to use concurrently per shard.
	//
	// A higher value decreases lock contention but consumes memory.
	RandomParallelism uint `yaml:"random_parallelism"`

	// GCInterval is the interval at which garbage collection will run.
	GCInterval time.Duration `yaml:"gc_interval"`

	// GCCutoff is the maximum duration a peer is allowed to go without
	// announcing before being marked for garbage collection.
	GCCutoff time.Duration `yaml:"gc_cutoff"`
}

func validateConfig(cfg Config) (Config, error) {
	if cfg.ShardCountBits < 1 {
		cfg.ShardCountBits = 10
	}

	if cfg.RandomParallelism < 1 {
		cfg.RandomParallelism = 8
	}

	if cfg.GCInterval == 0 {
		return cfg, ErrInvalidGCInterval
	}

	if cfg.GCCutoff == 0 {
		return cfg, ErrInvalidGCCutoff
	}

	return cfg, nil
}
