package optmem

import (
	"errors"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/chihaya/chihaya/server/store"
)

var (
	// ErrInvalidGCCutoff is returned for a config with an invalid
	// gc_cutoff.
	ErrInvalidGCCutoff = errors.New("invalid gc_cutoff")

	// ErrInvalidGCInterval is returned for a config with an invalid
	// gc_interval.
	ErrInvalidGCInterval = errors.New("invalid gc_interval")

	// ErrMissingConfig is returned if a non-existent config is opened.
	ErrMissingConfig = errors.New("missing config")
)

type peerStoreConfig struct {

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

	// GCInterval is the interval at which garbace collection will run.
	GCInterval time.Duration `yaml:"gc_interval"`

	// GCCutoff is the maximum duration a peer is allowed to go without
	// announcing before being marked for garbage collection.
	GCCutoff time.Duration `yaml:"gc_cutoff"`
}

func newPeerStoreConfig(storecfg *store.DriverConfig) (*peerStoreConfig, error) {
	if storecfg == nil || storecfg.Config == nil {
		return nil, ErrMissingConfig
	}

	bytes, err := yaml.Marshal(storecfg.Config)
	if err != nil {
		return nil, err
	}

	var cfg peerStoreConfig
	err = yaml.Unmarshal(bytes, &cfg)
	if err != nil {
		return nil, err
	}

	if cfg.ShardCountBits < 1 {
		cfg.ShardCountBits = 10
	}

	if cfg.GCInterval == 0 {
		return nil, ErrInvalidGCInterval
	}

	if cfg.GCCutoff == 0 {
		return nil, ErrInvalidGCCutoff
	}
	return &cfg, nil
}
