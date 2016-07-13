package optmem

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/chihaya/chihaya/server/store"
)

func TestNewPeerStoreConfig(t *testing.T) {
	cfg, err := newPeerStoreConfig(peerStoreTestConfig)
	require.Nil(t, err)
	require.NotNil(t, cfg)

	cfg, err = newPeerStoreConfig(nil)
	require.Equal(t, ErrMissingConfig, err)
	require.Nil(t, cfg)

	cfg, err = newPeerStoreConfig(&store.DriverConfig{})
	require.Equal(t, ErrMissingConfig, err)
	require.Nil(t, cfg)

	cfg, err = newPeerStoreConfig(&store.DriverConfig{Config: nil})
	require.Equal(t, ErrMissingConfig, err)
	require.Nil(t, cfg)

	cfg, err = newPeerStoreConfig(&store.DriverConfig{Config: peerStoreConfig{GCCutoff: time.Duration(50)}})
	require.Equal(t, ErrInvalidGCInterval, err)
	require.Nil(t, cfg)

	cfg, err = newPeerStoreConfig(&store.DriverConfig{Config: peerStoreConfig{GCInterval: time.Duration(50)}})
	require.Equal(t, ErrInvalidGCCutoff, err)
	require.Nil(t, cfg)

	bogus := struct {
		GCInterval string `yaml:"gc_interval"`
		GCCutoff   string `yaml:"gc_cutoff"`
	}{"invalid", "values"}

	cfg, err = newPeerStoreConfig(&store.DriverConfig{Config: bogus})
	require.NotNil(t, err)
	require.Nil(t, cfg)
}
