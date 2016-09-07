# chihaya-optmem-peerstore
A low-memory PeerStore for the chihaya BitTorrent tracker

[![Build Status](https://travis-ci.org/mrd0ll4r/chihaya-optmem-peerstore.svg?branch=master)](https://travis-ci.org/mrd0ll4r/chihaya-optmem-peerstore)
[![Go Report Card](https://goreportcard.com/badge/github.com/mrd0ll4r/chihaya-optmem-peerstore)](https://goreportcard.com/report/github.com/mrd0ll4r/chihaya-optmem-peerstore)
[![GoDoc](https://godoc.org/github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem?status.svg)](https://godoc.org/github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![IRC Channel](https://img.shields.io/badge/freenode-%23chihaya-blue.svg "IRC Channel")](http://webchat.freenode.net/?channels=chihaya)

## What is it?
An implementation of the `PeerStore` interface for [chihaya].
It uses very little memory, is (subjectively) fast and handles both IPv4 and IPv6 peers in separate swarms.

[chihaya]: https://github.com/chihaya/chihaya

## How do I use it?
You should first `go get` the relevant package:

```
go get github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem
```

Next you need to import it in your chihaya binary, like so:

```go
import "github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem"
```

Now modify your config to hold a config for `optmem` and parse it, then create the storage for the tracker backend from it.


## Configuration
A typical configuration of `optmem` would look like this:

```yaml
chihaya:
  announce_interval: 15m
  prometheus_addr: localhost:6880
#   ... more tracker config ... 

  storage:
    shard_count_bits: 10
    random_parallelism: 8
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
    
- `random_parallelism` specifies the number of parallel random sources to be used per shard.  
    A higher number improves lock contention but consumes a lot of memory.
    A value of zero will be interpreted as a value of 8.
    
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
Testing peer store "optmem", Config: map[shard_count_bits:10 gc_interval:5m gc_cutoff:5m random_parallelism:8]...
1 infohashes...
        1 seeders,         0 leechers:         44458976B (     43416KiB,     42.4MiB)

2 infohashes...
        1 seeders,         0 leechers:         44459568B (     43417KiB,     42.4MiB)

5 infohashes...
        1 seeders,         0 leechers:         44461088B (     43419KiB,     42.4MiB)

10 infohashes...
        1 seeders,         0 leechers:         44462560B (     43420KiB,     42.4MiB)

20 infohashes...
        1 seeders,         0 leechers:         44467280B (     43425KiB,     42.4MiB)

50 infohashes...
        1 seeders,         0 leechers:         44476768B (     43434KiB,     42.4MiB)

100 infohashes...
        1 seeders,         0 leechers:         44489520B (     43446KiB,     42.4MiB)

200 infohashes...
        1 seeders,         0 leechers:         44507808B (     43464KiB,     42.4MiB)

500 infohashes...
        1 seeders,         0 leechers:         44561600B (     43517KiB,     42.5MiB)

1000 infohashes...
        1 seeders,         0 leechers:         44667168B (     43620KiB,     42.6MiB)

2000 infohashes...
        1 seeders,         0 leechers:         44878896B (     43827KiB,     42.8MiB)

5000 infohashes...
        1 seeders,         0 leechers:         45514720B (     44447KiB,     43.4MiB)

10000 infohashes...
        1 seeders,         0 leechers:         46566704B (     45475KiB,     44.4MiB)

20000 infohashes...
        1 seeders,         0 leechers:         48625696B (     47486KiB,     46.4MiB)

50000 infohashes...
        1 seeders,         0 leechers:         54331392B (     53058KiB,     51.8MiB)

100000 infohashes...
        1 seeders,         0 leechers:         64149600B (     62646KiB,     61.2MiB)

200000 infohashes...
        1 seeders,         0 leechers:         83227648B (     81277KiB,     79.4MiB)

500000 infohashes...
        1 seeders,         0 leechers:        171585264B (    167563KiB,    163.6MiB)

1000000 infohashes...
        1 seeders,         0 leechers:        301919584B (    294843KiB,    287.9MiB)

2000000 infohashes...
        1 seeders,         0 leechers:        566868928B (    553582KiB,    540.6MiB)

5000000 infohashes...
        1 seeders,         0 leechers:       1047903088B (   1023342KiB,    999.4MiB)
```

And here are some memory usages for a lot of peers for a single infohash:

```
Testing peer store "optmem", Config: map[gc_interval:5m gc_cutoff:5m random_parallelism:8 shard_count_bits:10]...
1 infohashes...
        0 seeders,         1 leechers:              976B
        1 seeders,         1 leechers:              704B
        1 seeders,         1 leechers:              864B
        5 seeders,         5 leechers:              864B
       10 seeders,        10 leechers:             1424B (         1KiB)
       25 seeders,        25 leechers:             2208B (         2KiB)
       50 seeders,        50 leechers:             3536B (         3KiB)
      100 seeders,       100 leechers:             6368B (         6KiB)
      250 seeders,       250 leechers:            13296B (        12KiB)
      500 seeders,       500 leechers:            25616B (        25KiB)
     1000 seeders,      1000 leechers:            50048B (        48KiB)
     2500 seeders,      2500 leechers:           194704B (       190KiB)
     5000 seeders,      5000 leechers:           391984B (       382KiB)
    10000 seeders,     10000 leechers:           776976B (       758KiB)
    25000 seeders,     25000 leechers:          1579632B (      1542KiB,      1.5MiB)
    50000 seeders,     50000 leechers:          3158880B (      3084KiB,      3.0MiB)
   100000 seeders,    100000 leechers:          6321088B (      6172KiB,      6.0MiB)
   250000 seeders,    250000 leechers:         12663360B (     12366KiB,     12.1MiB)
   500000 seeders,    500000 leechers:         25295472B (     24702KiB,     24.1MiB)
  1000000 seeders,   1000000 leechers:         50565792B (     49380KiB,     48.2MiB)
  2500000 seeders,   2500000 leechers:        194642688B (    190080KiB,    185.6MiB)
  5000000 seeders,   5000000 leechers:        388961120B (    379844KiB,    370.9MiB)
 10000000 seeders,  10000000 leechers:        822362032B (    803087KiB,    784.3MiB)

```

Note that there are no differences between IPv4 and IPv6 peers regarding memory usage.

## License
MIT, see the LICENSE file
