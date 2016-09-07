package optmem

import (
	"net"
	"testing"
	"time"

	"github.com/chihaya/chihaya/bittorrent"
	s "github.com/chihaya/chihaya/storage"
	"github.com/stretchr/testify/require"
)

var (
	testConfig = Config{ShardCountBits: 10, RandomParallelism: 8, GCInterval: time.Duration(10000000000), GCCutoff: time.Duration(10000000000)}
)

var (
	ih = bittorrent.InfoHashFromString("00000000000000000000")
	p1 = bittorrent.Peer{
		IP:   net.ParseIP("1.2.3.4"),
		Port: 1234,
	}
	p2 = bittorrent.Peer{
		IP:   net.ParseIP("2.3.4.5"),
		Port: 2345,
	}
)

func TestPutNumGetSeeder(t *testing.T) {
	ps, err := New(testConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutSeeder(ih, p1)
	require.Nil(t, err)

	require.Equal(t, 1, ps.NumSeeders(ih))

	seeders4, _, err := ps.GetSeeders(ih)
	require.Nil(t, err)
	require.NotNil(t, seeders4)

	require.Equal(t, 1, len(seeders4))
	require.Equal(t, 4, len(seeders4[0].IP))
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
	ps, err := New(testConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutLeecher(ih, p1)
	require.Nil(t, err)

	require.Equal(t, 1, ps.NumLeechers(ih))

	leechers4, _, err := ps.GetLeechers(ih)
	require.Nil(t, err)
	require.NotNil(t, leechers4)

	require.Equal(t, 1, len(leechers4))
	require.Equal(t, 4, len(leechers4[0].IP))
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
	ps, err := New(testConfig)
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
	require.Equal(t, 4, len(seeders4[0].IP))
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
	ps, err := New(testConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutSeeder(ih, p1)
	require.Nil(t, err)

	err = ps.DeleteSeeder(ih, p1)
	require.Nil(t, err)

	require.Equal(t, 0, ps.NumSeeders(ih))

	_, _, err = ps.GetSeeders(ih)
	require.Equal(t, s.ErrResourceDoesNotExist, err)

	e := ps.Stop()
	err = <-e
	require.Nil(t, err)
}

func TestDeleteLeecher(t *testing.T) {
	ps, err := New(testConfig)
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
	require.Equal(t, 4, len(leechers4[0].IP))
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
	ps, err := New(testConfig)
	require.Nil(t, err)
	require.NotNil(t, ps)

	err = ps.PutLeecher(ih, p1)
	require.Nil(t, err)

	err = ps.DeleteLeecher(ih, p1)
	require.Nil(t, err)

	require.Equal(t, 0, ps.NumLeechers(ih))

	_, _, err = ps.GetLeechers(ih)
	require.Equal(t, s.ErrResourceDoesNotExist, err)

	e := ps.Stop()
	err = <-e
	require.Nil(t, err)
}

func createNew() s.PeerStore {
	ps, err := New(testConfig)
	if err != nil {
		panic(err)
	}
	return ps
}

func BenchmarkPut(b *testing.B)                        { s.Put(b, createNew()) }
func BenchmarkPut1k(b *testing.B)                      { s.Put1k(b, createNew()) }
func BenchmarkPut1kInfohash(b *testing.B)              { s.Put1kInfohash(b, createNew()) }
func BenchmarkPut1kInfohash1k(b *testing.B)            { s.Put1kInfohash1k(b, createNew()) }
func BenchmarkPutDelete(b *testing.B)                  { s.PutDelete(b, createNew()) }
func BenchmarkPutDelete1k(b *testing.B)                { s.PutDelete1k(b, createNew()) }
func BenchmarkPutDelete1kInfohash(b *testing.B)        { s.PutDelete1kInfohash(b, createNew()) }
func BenchmarkPutDelete1kInfohash1k(b *testing.B)      { s.PutDelete1kInfohash1k(b, createNew()) }
func BenchmarkDeleteNonexist(b *testing.B)             { s.DeleteNonexist(b, createNew()) }
func BenchmarkDeleteNonexist1k(b *testing.B)           { s.DeleteNonexist1k(b, createNew()) }
func BenchmarkDeleteNonexist1kInfohash(b *testing.B)   { s.DeleteNonexist1kInfohash(b, createNew()) }
func BenchmarkDeleteNonexist1kInfohash1k(b *testing.B) { s.DeleteNonexist1kInfohash1k(b, createNew()) }
func BenchmarkPutGradDelete(b *testing.B)              { s.PutGradDelete(b, createNew()) }
func BenchmarkPutGradDelete1k(b *testing.B)            { s.PutGradDelete1k(b, createNew()) }
func BenchmarkPutGradDelete1kInfohash(b *testing.B)    { s.PutGradDelete1kInfohash(b, createNew()) }
func BenchmarkPutGradDelete1kInfohash1k(b *testing.B)  { s.PutGradDelete1kInfohash1k(b, createNew()) }
func BenchmarkGradNonexist(b *testing.B)               { s.GradNonexist(b, createNew()) }
func BenchmarkGradNonexist1k(b *testing.B)             { s.GradNonexist1k(b, createNew()) }
func BenchmarkGradNonexist1kInfohash(b *testing.B)     { s.GradNonexist1kInfohash(b, createNew()) }
func BenchmarkGradNonexist1kInfohash1k(b *testing.B)   { s.GradNonexist1kInfohash1k(b, createNew()) }
func BenchmarkAnnounceLeecher(b *testing.B)            { s.AnnounceLeecher(b, createNew()) }
func BenchmarkAnnounceLeecher1kInfohash(b *testing.B)  { s.AnnounceLeecher1kInfohash(b, createNew()) }
func BenchmarkAnnounceSeeder(b *testing.B)             { s.AnnounceSeeder(b, createNew()) }
func BenchmarkAnnounceSeeder1kInfohash(b *testing.B)   { s.AnnounceSeeder1kInfohash(b, createNew()) }
