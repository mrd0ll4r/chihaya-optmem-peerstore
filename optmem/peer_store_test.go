package optmem

import (
	"testing"

	"time"

	"net"

	"github.com/chihaya/chihaya"
	"github.com/chihaya/chihaya/server/store"
	"github.com/stretchr/testify/require"
)

var (
	peerStoreTester      = store.PreparePeerStoreTester(&peerStoreDriver{})
	peerStoreBenchmarker = store.PreparePeerStoreBenchmarker(&peerStoreDriver{})
	peerStoreTestConfig  = &store.DriverConfig{Config: &peerStoreConfig{ShardCountBits: 10, GCInterval: time.Duration(10000000000), GCCutoff: time.Duration(10000000000)}}
)

var (
	ih = chihaya.InfoHashFromString("00000000000000000000")
	p1 = chihaya.Peer{
		IP:   net.ParseIP("1.2.3.4"),
		Port: 1234,
	}
	p2 = chihaya.Peer{
		IP:   net.ParseIP("2.3.4.5"),
		Port: 2345,
	}
)

func init() {
	peerStoreTester.CompareEndpoints()
}

func TestPutNumGetSeeder(t *testing.T) {
	ps, err := (&peerStoreDriver{}).New(peerStoreTestConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutSeeder(ih, p1)
	require.Nil(t, err)

	require.Equal(t, 1, ps.NumSeeders(ih))

	seeders4, _, err := ps.GetSeeders(ih)
	require.Nil(t, err)
	require.NotNil(t, seeders4)

	require.Equal(t, 1, len(seeders4))
	require.Equal(t, p1.Port, seeders4[0].Port)
	require.True(t, p1.IP.Equal(seeders4[0].IP))

	leechers4, leechers6, err := ps.GetLeechers(ih)
	require.Nil(t, err)
	if leechers4 != nil {
		require.Equal(t, 0, len(leechers4))
	}
	if leechers6 != nil {
		require.Equal(t, 0, len(leechers6))
	}

	e := ps.Stop()
	err = <-e
	require.Nil(t, err)
}

func TestPutNumGetLeecher(t *testing.T) {
	ps, err := (&peerStoreDriver{}).New(peerStoreTestConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutLeecher(ih, p1)
	require.Nil(t, err)

	require.Equal(t, 1, ps.NumLeechers(ih))

	leechers4, _, err := ps.GetLeechers(ih)
	require.Nil(t, err)
	require.NotNil(t, leechers4)

	require.Equal(t, 1, len(leechers4))
	require.Equal(t, p1.Port, leechers4[0].Port)
	require.True(t, p1.IP.Equal(leechers4[0].IP))

	seeders4, seeders6, err := ps.GetSeeders(ih)
	require.Nil(t, err)
	if seeders4 != nil {
		require.Equal(t, 0, len(seeders4))
	}
	if seeders6 != nil {
		require.Equal(t, 0, len(seeders6))
	}

	e := ps.Stop()
	err = <-e
	require.Nil(t, err)
}

func TestDeleteSeeder(t *testing.T) {
	ps, err := (&peerStoreDriver{}).New(peerStoreTestConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutSeeder(ih, p1)
	require.Nil(t, err)

	err = ps.PutSeeder(ih, p2)
	require.Nil(t, err)

	require.Equal(t, 2, ps.NumSeeders(ih))

	err = ps.DeleteSeeder(ih, p2)
	require.Nil(t, err)

	require.Equal(t, 1, ps.NumSeeders(ih))

	seeders4, _, err := ps.GetSeeders(ih)
	require.Nil(t, err)
	require.NotNil(t, seeders4)

	require.Equal(t, 1, len(seeders4))
	require.Equal(t, p1.Port, seeders4[0].Port)
	require.True(t, p1.IP.Equal(seeders4[0].IP))

	leechers4, leechers6, err := ps.GetLeechers(ih)
	require.Nil(t, err)
	if leechers4 != nil {
		require.Equal(t, 0, len(leechers4))
	}
	if leechers6 != nil {
		require.Equal(t, 0, len(leechers6))
	}

	e := ps.Stop()
	err = <-e
	require.Nil(t, err)
}

func TestDeleteLastSeeder(t *testing.T) {
	ps, err := (&peerStoreDriver{}).New(peerStoreTestConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutSeeder(ih, p1)
	require.Nil(t, err)

	err = ps.DeleteSeeder(ih, p1)
	require.Nil(t, err)

	require.Equal(t, 0, ps.NumSeeders(ih))

	_, _, err = ps.GetSeeders(ih)
	require.Equal(t, store.ErrResourceDoesNotExist, err)

	e := ps.Stop()
	err = <-e
	require.Nil(t, err)
}

func TestDeleteLeecher(t *testing.T) {
	ps, err := (&peerStoreDriver{}).New(peerStoreTestConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutLeecher(ih, p1)
	require.Nil(t, err)

	err = ps.PutLeecher(ih, p2)
	require.Nil(t, err)

	require.Equal(t, 2, ps.NumLeechers(ih))

	err = ps.DeleteLeecher(ih, p2)
	require.Nil(t, err)

	require.Equal(t, 1, ps.NumLeechers(ih))

	leechers4, _, err := ps.GetLeechers(ih)
	require.Nil(t, err)
	require.NotNil(t, leechers4)

	require.Equal(t, 1, len(leechers4))
	require.Equal(t, p1.Port, leechers4[0].Port)
	require.True(t, p1.IP.Equal(leechers4[0].IP))

	seeders4, seeders6, err := ps.GetSeeders(ih)
	require.Nil(t, err)
	if seeders4 != nil {
		require.Equal(t, 0, len(seeders4))
	}
	if seeders6 != nil {
		require.Equal(t, 0, len(seeders6))
	}

	e := ps.Stop()
	err = <-e
	require.Nil(t, err)
}

func TestDeleteLastLeecher(t *testing.T) {
	ps, err := (&peerStoreDriver{}).New(peerStoreTestConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutLeecher(ih, p1)
	require.Nil(t, err)

	err = ps.DeleteLeecher(ih, p1)
	require.Nil(t, err)

	require.Equal(t, 0, ps.NumLeechers(ih))

	_, _, err = ps.GetLeechers(ih)
	require.Equal(t, store.ErrResourceDoesNotExist, err)

	e := ps.Stop()
	err = <-e
	require.Nil(t, err)
}

func TestPeerStore(t *testing.T) {
	peerStoreTester.TestPeerStore(t, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutSeeder(b *testing.B) {
	peerStoreBenchmarker.PutSeeder(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutSeeder1KInfohash(b *testing.B) {
	peerStoreBenchmarker.PutSeeder1KInfohash(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutSeeder1KSeeders(b *testing.B) {
	peerStoreBenchmarker.PutSeeder1KSeeders(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutSeeder1KInfohash1KSeeders(b *testing.B) {
	peerStoreBenchmarker.PutSeeder1KInfohash1KSeeders(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutDeleteSeeder(b *testing.B) {
	peerStoreBenchmarker.PutDeleteSeeder(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutDeleteSeeder1KInfohash(b *testing.B) {
	peerStoreBenchmarker.PutDeleteSeeder1KInfohash(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutDeleteSeeder1KSeeders(b *testing.B) {
	peerStoreBenchmarker.PutDeleteSeeder1KSeeders(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutDeleteSeeder1KInfohash1KSeeders(b *testing.B) {
	peerStoreBenchmarker.PutDeleteSeeder1KInfohash1KSeeders(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_DeleteSeederNonExist(b *testing.B) {
	peerStoreBenchmarker.DeleteSeederNonExist(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_DeleteSeederNonExist1KInfohash(b *testing.B) {
	peerStoreBenchmarker.DeleteSeederNonExist1KInfohash(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_DeleteSeederNonExist1KSeeders(b *testing.B) {
	peerStoreBenchmarker.DeleteSeederNonExist1KSeeders(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_DeleteSeederNonExist1KInfohash1KSeeders(b *testing.B) {
	peerStoreBenchmarker.DeleteSeederNonExist1KInfohash1KSeeders(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutGraduateDeleteLeecher(b *testing.B) {
	peerStoreBenchmarker.PutGraduateDeleteLeecher(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutGraduateDeleteLeecher1KInfohash(b *testing.B) {
	peerStoreBenchmarker.PutGraduateDeleteLeecher1KInfohash(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutGraduateDeleteLeecher1KSeeders(b *testing.B) {
	peerStoreBenchmarker.PutGraduateDeleteLeecher1KLeechers(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_PutGraduateDeleteLeecher1KInfohash1KSeeders(b *testing.B) {
	peerStoreBenchmarker.PutGraduateDeleteLeecher1KInfohash1KLeechers(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_GraduateLeecherNonExist(b *testing.B) {
	peerStoreBenchmarker.GraduateLeecherNonExist(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_GraduateLeecherNonExist1KInfohash(b *testing.B) {
	peerStoreBenchmarker.GraduateLeecherNonExist1KInfohash(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_GraduateLeecherNonExist1KSeeders(b *testing.B) {
	peerStoreBenchmarker.GraduateLeecherNonExist1KLeechers(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_GraduateLeecherNonExist1KInfohash1KSeeders(b *testing.B) {
	peerStoreBenchmarker.GraduateLeecherNonExist1KInfohash1KLeechers(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_AnnouncePeers(b *testing.B) {
	peerStoreBenchmarker.AnnouncePeers(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_AnnouncePeers1KInfohash(b *testing.B) {
	peerStoreBenchmarker.AnnouncePeers1KInfohash(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_AnnouncePeersSeeder(b *testing.B) {
	peerStoreBenchmarker.AnnouncePeersSeeder(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_AnnouncePeersSeeder1KInfohash(b *testing.B) {
	peerStoreBenchmarker.AnnouncePeersSeeder1KInfohash(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_GetSeeders(b *testing.B) {
	peerStoreBenchmarker.GetSeeders(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_GetSeeders1KInfohash(b *testing.B) {
	peerStoreBenchmarker.GetSeeders1KInfohash(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_NumSeeders(b *testing.B) {
	peerStoreBenchmarker.NumSeeders(b, peerStoreTestConfig)
}

func BenchmarkPeerStore_NumSeeders1KInfohash(b *testing.B) {
	peerStoreBenchmarker.NumSeeders1KInfohash(b, peerStoreTestConfig)
}
