package optmem

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/chihaya/chihaya/server/store"
)

func TestNewPeerStoreConfig(t *testing.T) {
	cfg, err := newPeerStoreConfig(&store.DriverConfig{Config: peerStoreConfig{ShardCountBits: 11}})
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
}
