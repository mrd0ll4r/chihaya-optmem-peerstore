package optmem

import (
	"encoding/binary"
	"fmt"
)

type infohash [20]byte

const ipLen = 16   // 16-byte IPv6 address
const portLen = 2  // uint16 port
const flagLen = 1  // 1-byte seeder/leecher flag
const mtimeLen = 2 // uint16(unix seconds) last modified time

type peer struct {
	data [ipLen + portLen + flagLen + mtimeLen]byte // use byte-array instead of byte-slice, save a few header bytes!
}

// setIP sets the IP-bytes of a peer to a copy of the bytes specified.
func (p *peer) setIP(ip []byte) {
	if len(ip) != ipLen {
		panic(fmt.Sprintf("ip with %d bytes expected, got %d", ipLen, len(ip)))
	}
	copy(p.data[:ipLen], ip)
}

// ip returns a copy of the IP-bytes of a peer
func (p *peer) ip() []byte {
	toReturn := make([]byte, ipLen)
	copy(toReturn, p.data[:ipLen])
	return toReturn
}

func (p *peer) setPort(port uint16) {
	binary.BigEndian.PutUint16(p.data[ipLen:ipLen+portLen], port)
}

func (p *peer) port() uint16 {
	return binary.BigEndian.Uint16(p.data[ipLen : ipLen+portLen])
}

func (p *peer) peerFlag() peerFlag {
	return peerFlag(p.data[ipLen+portLen])
}

func (p *peer) setPeerFlag(to peerFlag) {
	p.data[ipLen+portLen] = byte(to)
}

func (p *peer) peerTime() uint16 {
	return binary.BigEndian.Uint16(p.data[ipLen+portLen+flagLen:])
}

func (p *peer) setPeerTime(to uint16) {
	binary.BigEndian.PutUint16(p.data[ipLen+portLen+flagLen:], to)
}

func (p *peer) isSeeder() bool {
	return p.peerFlag()&peerFlagSeeder != 0
}

func (p *peer) isLeecher() bool {
	return p.peerFlag()&peerFlagLeecher != 0
}

type peerFlag byte

const (
	peerFlagSeeder peerFlag = 1 << iota
	peerFlagLeecher
)

type swarm struct {
	peers *peerList
}

type shard struct {
	swarms map[infohash]swarm
}
