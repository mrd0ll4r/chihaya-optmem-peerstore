package optmem

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewPeerStoreConfig(t *testing.T) {
	_, err := validateConfig(testConfig)
	require.Nil(t, err)

	_, err = validateConfig(Config{PeerLifetime: time.Duration(50)})
	require.Equal(t, ErrInvalidPeerLifetime, err)

	_, err = validateConfig(Config{GCInterval: time.Duration(50)})
	require.Equal(t, ErrInvalidGCCutoff, err)
}
