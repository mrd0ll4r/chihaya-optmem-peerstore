package optmem

import (
	"errors"
	"time"

	"github.com/chihaya/chihaya/pkg/log"
)

var (
	// ErrInvalidGCCutoff is returned for a config with an invalid
	// gc_cutoff.
	ErrInvalidGCCutoff = errors.New("invalid gc_cutoff")

	// ErrInvalidPeerLifetime is returned for a config with an invalid
	// peer_lifetime.
	ErrInvalidPeerLifetime = errors.New("invalid peer_lifetime")
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

	// GCInterval is the interval at which garbage collection will run.
	GCInterval time.Duration `yaml:"gc_interval"`

	// PeerLifetime is the maximum duration a peer is allowed to go without
	// announcing before being marked for garbage collection.
	PeerLifetime time.Duration `yaml:"peer_lifetime"`

	PrometheusReportingInterval time.Duration `yaml:"prometheus_reporting_interval"`
}

// LogFields implements log.LogFielder for a Config.
func (c Config) LogFields() log.Fields {
	return log.Fields{
		"shardCountBits":              c.ShardCountBits,
		"gcInterval":                  c.GCInterval,
		"peerLifetime":                c.PeerLifetime,
		"prometheusReportingInterval": c.PrometheusReportingInterval,
	}
}

func validateConfig(cfg Config) (Config, error) {
	if cfg.ShardCountBits < 1 {
		cfg.ShardCountBits = 10
	}

	if cfg.GCInterval == 0 {
		return cfg, ErrInvalidPeerLifetime
	}

	if cfg.PeerLifetime == 0 {
		return cfg, ErrInvalidGCCutoff
	}

	return cfg, nil
}
