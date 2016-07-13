# chihaya-optmem-peerstore
A low-memory PeerStore for the chihaya BitTorrent tracker

[![Build Status](https://travis-ci.org/mrd0ll4r/chihaya-optmem-peerstore.svg?branch=master)](https://travis-ci.org/mrd0ll4r/chihaya-optmem-peerstore)
[![Go Report Card](https://goreportcard.com/badge/github.com/mrd0ll4r/chihaya-optmem-peerstore)](https://goreportcard.com/report/github.com/mrd0ll4r/chihaya-optmem-peerstore)
[![GoDoc](https://godoc.org/github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem?status.svg)](https://godoc.org/github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![IRC Channel](https://img.shields.io/badge/freenode-%23chihaya-blue.svg "IRC Channel")](http://webchat.freenode.net/?channels=chihaya)

## What is it?
An implementation of the `PeerStore` interface for [chihaya].
It uses very little memory, is (subjectively) fast and handles both IPv4 and IPv6 peers in mixed swarms.

It registers itself as `optmem` with the chihaya `store`.

[chihaya]: https://github.com/chihaya/chihaya

## How do I use it?
You should first `go get` the relevant package:

```
go get github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem
```

Next you need to import it in your chihaya binary, like so:

```go
import _ github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem
```

Now you can use it by configuring the `store` to use `optmem` as the PeerStore driver.


## Configuration
A typical configuration of `optmem` would look like this:

```yaml
chihaya:
  tracker:
    announce: 10m
    min_announce: 5m
#   ... more tracker config ... 

  servers:
    - name: store
      config:
#        ... more store config ...
        peer_store:
          name: optmem
          config:
            shard_count_bits: 10
            gc_interval: 2m
            gc_cutoff: 12m

# ... more configuration ...
```

Where the parameters are:

- `shard_count_bits` specifies the number of bits to use to index shards (parts of the infohash key space).  
    For example:
    - `shard_count_bits: 1` will create two shards, each responsible for half of all possible infohashes
    - `shard_count_bits: 2` will create four shards, each responsible for a quarter of all possible infohashes
    - `shard_count_bits: 10` will create 1024 shards, each responsible for 1/1024th of all possible infohashes
    - `shard_count_bits: 0` will be interpreted as `shard_count_bits: 10`
    
    Creating a shard takes a small amount of memory, even without actually storing any infohashes.
    Having many shards therefore increases the base memory usage of the peer store, but does not affect the amount of memory a single infohash takes.
    Partitioning the key space into shards has the advantage of being able to lock each shard seperately, which can be a major performance boost.
    Having many shards therefore also decreases lock contention.
    
    Unless you really know what you're doing, using at least 1024 shards is recommended.
    
- `gc_interval` is the interval at which (or rather: the pause between) garbage collection runs.  
    Garbage collection collects peers that have not announced for a certain amount of time and empty swarms.
    
- `gc_cutoff` is the maximum duration a peer is allowed to go without announcing before being marked for garbage collection.  
    A low multiple of the announce interval is recommended.
    For example: If the announce interval is 10 minutes, choose 11 to 15 minutes for the `gc_cutoff`.

## Limitations
This `PeerStore` does not save PeerIDs.
They take 20 bytes per peer and are only ever returned in non-compact HTTP announces.

The timestamp used for garbage collection is in seconds and stored in an unsigned 16-bit integer.
This limits the maximum age of peers to have working garbage collection.
Determining the limit is left as an exercise for the reader.

## Data representation
Each shard is a lockable map of infohashes to their swarms.
This allows for smaller locks and more concurrency.

Each swarm is a struct that contains the number of peers, the number of seeders and the number of completed downloads.
Also, each swarm contains a slice of slices of peers (a list of "buckets").

Each bucket is a sorted (by IP) array of peers.
The number of buckets is dynamically adjusted to minimize huge memory moves/reallocations when a peer has to be inserted/removed.

Each peer is a byte array, a concatenation of its IP (as an IPv6 address), Port, a flag indicating what function the peer has (leecher or seeder) and a 16-bit timestamp for when the peer last announced in unix seconds.

The data representation is largely inspired by [opentracker].
Make sure to check it out.
Thanks to erdgeist for that great piece of software!

[opentracker]: https://erdgeist.org/arts/software/opentracker/

## Performance
Here are some memory usages for many infohashes:

```
Testing peer store "optmem", Config: map[shard_count_bits:11]...
1 infohashes...
        1 seeders,         0 leechers:           148736B (       145KiB)

2 infohashes...
        1 seeders,         0 leechers:           149136B (       145KiB)

5 infohashes...
        1 seeders,         0 leechers:           150208B (       146KiB)

10 infohashes...
        1 seeders,         0 leechers:           152048B (       148KiB)

20 infohashes...
        1 seeders,         0 leechers:           155728B (       152KiB)

50 infohashes...
        1 seeders,         0 leechers:           164432B (       160KiB)

100 infohashes...
        1 seeders,         0 leechers:           178448B (       174KiB)

200 infohashes...
        1 seeders,         0 leechers:           200016B (       195KiB)

500 infohashes...
        1 seeders,         0 leechers:           250528B (       244KiB)

1000 infohashes...
        1 seeders,         0 leechers:           329424B (       321KiB)

2000 infohashes...
        1 seeders,         0 leechers:           515008B (       502KiB)

5000 infohashes...
        1 seeders,         0 leechers:          1093040B (      1067KiB,      1.0MiB)

10000 infohashes...
        1 seeders,         0 leechers:          2084128B (      2035KiB,      2.0MiB)

20000 infohashes...
        1 seeders,         0 leechers:          4010384B (      3916KiB,      3.8MiB)

50000 infohashes...
        1 seeders,         0 leechers:          9297856B (      9079KiB,      8.9MiB)

100000 infohashes...
        1 seeders,         0 leechers:         18414816B (     17983KiB,     17.6MiB)

200000 infohashes...
        1 seeders,         0 leechers:         36629936B (     35771KiB,     34.9MiB)

500000 infohashes...
        1 seeders,         0 leechers:        110865616B (    108267KiB,    105.7MiB)

1000000 infohashes...
        1 seeders,         0 leechers:        226571968B (    221261KiB,    216.1MiB)

2000000 infohashes...
        1 seeders,         0 leechers:        462031712B (    451202KiB,    440.6MiB)

5000000 infohashes...
        1 seeders,         0 leechers:        926447680B (    904734KiB,    883.5MiB)
```

And here are some memory usages for a lot of peers for a single infohash:

```
Testing peer store "optmem", Config: map[shard_count_bits:11]...
1 infohashes...
        1 seeders,         1 leechers:           148768B (       145KiB)
        5 seeders,         5 leechers:           148976B (       145KiB)
       10 seeders,        10 leechers:           149408B (       145KiB)
       25 seeders,        25 leechers:           150176B (       146KiB)
       50 seeders,        50 leechers:           151520B (       147KiB)
      100 seeders,       100 leechers:           154464B (       150KiB)
      250 seeders,       250 leechers:           160992B (       157KiB)
      500 seeders,       500 leechers:           173424B (       169KiB)
     1000 seeders,      1000 leechers:           198000B (       193KiB)
     2500 seeders,      2500 leechers:           342704B (       334KiB)
     5000 seeders,      5000 leechers:           543440B (       530KiB)
    10000 seeders,     10000 leechers:           925040B (       903KiB)
    25000 seeders,     25000 leechers:          1727824B (      1687KiB,      1.6MiB)
    50000 seeders,     50000 leechers:          3306992B (      3229KiB,      3.2MiB)
   100000 seeders,    100000 leechers:          6469792B (      6318KiB,      6.2MiB)
   250000 seeders,    250000 leechers:         12811632B (     12511KiB,     12.2MiB)
   500000 seeders,    500000 leechers:         25443600B (     24847KiB,     24.3MiB)
  1000000 seeders,   1000000 leechers:         50713776B (     49525KiB,     48.4MiB)
  2500000 seeders,   2500000 leechers:        194790320B (    190224KiB,    185.8MiB)
  5000000 seeders,   5000000 leechers:        389109424B (    379989KiB,    371.1MiB)
 10000000 seeders,  10000000 leechers:        778049712B (    759814KiB,    742.0MiB)
```

Note that there are no differences between IPv4 and IPv6 peers regarding memory usage.

## License
MIT, see the LICENSE file
