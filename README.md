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
Testing peer store "optmem", Config: map[shard_count_bits:10 gc_interval:5m gc_cutoff:5m]...
1 infohashes...
        1 seeders,         0 leechers:            74624B (        72KiB)

2 infohashes...
        1 seeders,         0 leechers:            75104B (        73KiB)

5 infohashes...
        1 seeders,         0 leechers:            76416B (        74KiB)

10 infohashes...
        1 seeders,         0 leechers:            78336B (        76KiB)

20 infohashes...
        1 seeders,         0 leechers:            82816B (        80KiB)

50 infohashes...
        1 seeders,         0 leechers:            92480B (        90KiB)

100 infohashes...
        1 seeders,         0 leechers:           104896B (       102KiB)

200 infohashes...
        1 seeders,         0 leechers:           123456B (       120KiB)

500 infohashes...
        1 seeders,         0 leechers:           177216B (       173KiB)

1000 infohashes...
        1 seeders,         0 leechers:           208656B (       203KiB)

2000 infohashes...
        1 seeders,         0 leechers:           418576B (       408KiB)

5000 infohashes...
        1 seeders,         0 leechers:          1122368B (      1096KiB,      1.1MiB)

10000 infohashes...
        1 seeders,         0 leechers:          2110480B (      2061KiB,      2.0MiB)

20000 infohashes...
        1 seeders,         0 leechers:          4156368B (      4058KiB,      4.0MiB)

50000 infohashes...
        1 seeders,         0 leechers:          9873552B (      9642KiB,      9.4MiB)

100000 infohashes...
        1 seeders,         0 leechers:         19763360B (     19300KiB,     18.8MiB)

200000 infohashes...
        1 seeders,         0 leechers:         38858432B (     37947KiB,     37.1MiB)

500000 infohashes...
        1 seeders,         0 leechers:        127171472B (    124190KiB,    121.3MiB)

1000000 infohashes...
        1 seeders,         0 leechers:        257554432B (    251518KiB,    245.6MiB)

2000000 infohashes...
        1 seeders,         0 leechers:        522502752B (    510256KiB,    498.3MiB)

5000000 infohashes...
        1 seeders,         0 leechers:       1003673984B (    980150KiB,    957.2MiB)
```

And here are some memory usages for a lot of peers for a single infohash:

```
Testing peer store "optmem", Config: map[shard_count_bits:10 gc_interval:5m gc_cutoff:5m]...
1 infohashes...
        1 seeders,         1 leechers:              480B
        5 seeders,         5 leechers:              880B
       10 seeders,        10 leechers:             1312B (         1KiB)
       25 seeders,        25 leechers:             2080B (         2KiB)
       50 seeders,        50 leechers:             3552B (         3KiB)
      100 seeders,       100 leechers:            80224B (        78KiB)
      250 seeders,       250 leechers:            13024B (        12KiB)
      500 seeders,       500 leechers:            99216B (        96KiB)
     1000 seeders,      1000 leechers:            50032B (        48KiB)
     2500 seeders,      2500 leechers:           194736B (       190KiB)
     5000 seeders,      5000 leechers:           392112B (       382KiB)
    10000 seeders,     10000 leechers:           777968B (       759KiB)
    25000 seeders,     25000 leechers:          1579856B (      1542KiB,      1.5MiB)
    50000 seeders,     50000 leechers:          3158832B (      3084KiB,      3.0MiB)
   100000 seeders,    100000 leechers:          6321440B (      6173KiB,      6.0MiB)
   250000 seeders,    250000 leechers:         12737488B (     12438KiB,     12.1MiB)
   500000 seeders,    500000 leechers:         25369392B (     24774KiB,     24.2MiB)
  1000000 seeders,   1000000 leechers:         50567536B (     49382KiB,     48.2MiB)
  2500000 seeders,   2500000 leechers:        194716208B (    190152KiB,    185.7MiB)
  5000000 seeders,   5000000 leechers:        389035056B (    379917KiB,    371.0MiB)
 10000000 seeders,  10000000 leechers:        777975760B (    759741KiB,    741.9MiB)
```

Note that there are no differences between IPv4 and IPv6 peers regarding memory usage.

## License
MIT, see the LICENSE file
