package optmem

import (
	"time"

	"github.com/chihaya/chihaya/pkg/log"
	"github.com/chihaya/chihaya/storage"
	"gopkg.in/yaml.v2"
)

// Name is the name of this storage.
const Name = "optmem"

// Default config constants.
const (
	defaultShardCountBits              = 10
	defaultPrometheusReportingInterval = time.Second * 1
	defaultGarbageCollectionInterval   = time.Minute * 3
	defaultPeerLifetime                = time.Minute * 30
)

func init() {
	// Register the storage driver.
	storage.RegisterDriver(Name, driver{})
}

type driver struct{}

func (d driver) NewPeerStore(icfg interface{}) (storage.PeerStore, error) {
	// Marshal the config back into bytes.
	bytes, err := yaml.Marshal(icfg)
	if err != nil {
		return nil, err
	}

	// Unmarshal the bytes into the proper config type.
	var cfg Config
	err = yaml.Unmarshal(bytes, &cfg)
	if err != nil {
		return nil, err
	}

	return New(cfg)
}

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

	// GarbageCollectionInterval is the interval at which garbage collection will run.
	GarbageCollectionInterval time.Duration `yaml:"gc_interval"`

	// PeerLifetime is the maximum duration a peer is allowed to go without
	// announcing before being marked for garbage collection.
	PeerLifetime time.Duration `yaml:"peer_lifetime"`

	// PrometheusReportingInterval is the interval at which metrics will be
	// aggregated and reported to prometheus.
	PrometheusReportingInterval time.Duration `yaml:"prometheus_reporting_interval"`
}

// LogFields implements log.LogFielder for a Config.
func (cfg Config) LogFields() log.Fields {
	return log.Fields{
		"shardCountBits":              cfg.ShardCountBits,
		"gcInterval":                  cfg.GarbageCollectionInterval,
		"peerLifetime":                cfg.PeerLifetime,
		"prometheusReportingInterval": cfg.PrometheusReportingInterval,
	}
}

// Validate sanity checks values set in a config and returns a new config with
// default values replacing anything that is invalid.
//
// This function warns to the logger when a value is changed.
func (cfg Config) Validate() Config {
	validcfg := cfg

	if cfg.ShardCountBits <= 0 {
		validcfg.ShardCountBits = defaultShardCountBits
		log.Warn("falling back to default configuration", log.Fields{
			"name":     Name + ".ShardCountBits",
			"provided": cfg.ShardCountBits,
			"default":  validcfg.ShardCountBits,
		})
	}

	if cfg.GarbageCollectionInterval <= 0 {
		validcfg.GarbageCollectionInterval = defaultGarbageCollectionInterval
		log.Warn("falling back to default configuration", log.Fields{
			"name":     Name + ".GarbageCollectionInterval",
			"provided": cfg.GarbageCollectionInterval,
			"default":  validcfg.GarbageCollectionInterval,
		})
	}

	if cfg.PrometheusReportingInterval <= 0 {
		validcfg.PrometheusReportingInterval = defaultPrometheusReportingInterval
		log.Warn("falling back to default configuration", log.Fields{
			"name":     Name + ".PrometheusReportingInterval",
			"provided": cfg.PrometheusReportingInterval,
			"default":  validcfg.PrometheusReportingInterval,
		})
	}

	if cfg.PeerLifetime <= 0 {
		validcfg.PeerLifetime = defaultPeerLifetime
		log.Warn("falling back to default configuration", log.Fields{
			"name":     Name + ".PeerLifetime",
			"provided": cfg.PeerLifetime,
			"default":  validcfg.PeerLifetime,
		})
	}

	return validcfg
}
