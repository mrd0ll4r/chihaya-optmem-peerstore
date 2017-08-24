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
go get -u github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem
```

Next you need to import it in your chihaya binary, like so:

```go
import _ "github.com/mrd0ll4r/chihaya-optmem-peerstore/optmem"
```

Now modify your config to use `optmem` as the storage, see the config below for an example.


## Configuration
A typical configuration could look like this:

```yaml
chihaya:
  announce_interval: 15m
  prometheus_addr: localhost:6880
#   ... more tracker config ... 

  storage:
    name: optmem
    config:
      shard_count_bits: 10
      gc_interval: 2m
      peer_lifetime: 16m
      prometheus_reporting_interval: 1s

# ... more configuration ...
```

Where the parameters are:

- `shard_count_bits` specifies the number of bits to use to index shards (parts of the infohash key space).  
    The peer store will create 2 to the power of `shard_count_bits` shards.
    For example:
    - `shard_count_bits: 1` will create two shards, each responsible for half of all possible infohashes
    - `shard_count_bits: 2` will create four shards, each responsible for a quarter of all possible infohashes
    - `shard_count_bits: 10` will create 1024 shards, each responsible for 1/1024th of all possible infohashes
    - `shard_count_bits: 0` will default to `shard_count_bits: 10`
    
    Creating a shard takes a small amount of memory, even without actually storing any infohashes.
    Having many shards therefore increases the base memory usage of the peer store, but does not affect the amount of memory a single infohash takes.
    Partitioning the key space into shards has the advantage of being able to lock each shard seperately, which can be a major performance boost.
    Having many shards therefore also decreases lock contention.
    
    Unless you really know what you're doing, using at least 1024 shards is recommended.
    
- `gc_interval` is the interval at which (or rather: the pause between) garbage collection runs.  
    Garbage collection collects peers that have not announced for a certain amount of time and empty swarms.
    
- `peer_lifetime` is the maximum duration a peer is allowed to go without announcing before being marked for garbage collection.  
    A low multiple of the announce interval is recommended.
    For example: If the announce interval is 10 minutes, choose 11 to 15 minutes for the `peer_lifetime`.

- `prometheus_reporting_interval` is the interval at which metrics will be aggregated and reported to Prometheus.  
    Collecting these metrics, although it's usually very fast, runs in linear time in regards to the number of swarms (=infohashes) tracked.
    If your tracker is very large, it might be beneficial to increase the reporting interval.

## Limitations
This `PeerStore` does not save PeerIDs.
They take 20 bytes per peer and are only ever returned in non-compact HTTP announces.

The timestamp used for garbage collection is in seconds and stored in an unsigned 16-bit integer.
This limits the maximum age of peers to have working garbage collection.
Determining the limit is left as an exercise for the reader.

## Data representation
The peer store holds a list of shards, each responsible for a fraction of the entire keyspace of possible infohashes.

Each shard is a lockable map of infohashes to their swarms.
This allows for smaller locks and more concurrency.

Each swarm is a struct that contains the number of peers, the number of seeders and the number of completed downloads.
Also, each swarm contains a slice of slices of peers (a list of "buckets").

Each bucket is a sorted (by IP) array of peers.
The number of buckets is dynamically adjusted to minimize huge memory moves/reallocations when a peer has to be inserted/removed.

Each peer is a byte array, a concatenation of its IP (as an IPv6 address), Port, a flag indicating what function the peer has (leecher or seeder) and a 16-bit timestamp for when the peer last announced in unix seconds.

The data representation is largely inspired by [opentracker].
Make sure to check it out.
Thanks to erdgeist for opentracker and allowing me to reuse a bunch of the data structures!

[opentracker]: https://erdgeist.org/arts/software/opentracker/

## Performance
Note that the method to determine the amount of memory used is imprecise, especially for small amounts of memory.

Here are some memory usages for many infohashes:

```
Testing peer store "optmem", Config: map[shard_count_bits:10 gc_interval:5m peer_lifetime:5m prometheus_reporting_interval:5m]...
1 infohashes...
        1 seeders,         0 leechers:	         108016B (       105KiB)

2 infohashes...
        1 seeders,         0 leechers:	         108496B (       105KiB)

5 infohashes...
        1 seeders,         0 leechers:	         109840B (       107KiB)

10 infohashes...
        1 seeders,         0 leechers:	           5312B (         5KiB)

20 infohashes...
        1 seeders,         0 leechers:	           9312B (         9KiB)

50 infohashes...
        1 seeders,         0 leechers:	         126000B (       123KiB)

100 infohashes...
        1 seeders,         0 leechers:	          32832B (        32KiB)

200 infohashes...
        1 seeders,         0 leechers:	         159696B (       155KiB)

500 infohashes...
        1 seeders,         0 leechers:	         211440B (       206KiB)

1000 infohashes...
        1 seeders,         0 leechers:	         314352B (       306KiB)

2000 infohashes...
        1 seeders,         0 leechers:	         531408B (       518KiB)

5000 infohashes...
        1 seeders,         0 leechers:	        1024416B (      1000KiB)

10000 infohashes...
        1 seeders,         0 leechers:	        2033904B (      1986KiB,      1.9MiB)

20000 infohashes...
        1 seeders,         0 leechers:	        4258192B (      4158KiB,      4.1MiB)

50000 infohashes...
        1 seeders,         0 leechers:	        9819472B (      9589KiB,      9.4MiB)

100000 infohashes...
        1 seeders,         0 leechers:	       19680000B (     19218KiB,     18.8MiB)

200000 infohashes...
        1 seeders,         0 leechers:	       38821056B (     37911KiB,     37.0MiB)

500000 infohashes...
        1 seeders,         0 leechers:	      127085184B (    124106KiB,    121.2MiB)

1000000 infohashes...
        1 seeders,         0 leechers:	      257648992B (    251610KiB,    245.7MiB)

2000000 infohashes...
        1 seeders,         0 leechers:	      522542576B (    510295KiB,    498.3MiB)

5000000 infohashes...
        1 seeders,         0 leechers:	     1003537520B (    980017KiB,    957.0MiB)

10000000 infohashes...
        1 seeders,         0 leechers:	     2007018496B (   1959979KiB,   1914.0MiB, 1.9GiB)
```

And here are some memory usages for a lot of peers for a single infohash:

```
Testing peer store "optmem", Config: map[shard_count_bits:10 gc_interval:5m peer_lifetime:5m prometheus_reporting_interval:5m]...
1 infohashes...
        0 seeders,         1 leechers:	           1088B (         1KiB)
        1 seeders,         1 leechers:	           1312B (         1KiB)
        1 seeders,         1 leechers:	           1504B (         1KiB)
        5 seeders,         5 leechers:	           1200B (         1KiB)
       10 seeders,        10 leechers:	           1424B (         1KiB)
       25 seeders,        25 leechers:	           2240B (         2KiB)
       50 seeders,        50 leechers:	           4064B (         3KiB)
      100 seeders,       100 leechers:	           7392B (         7KiB)
      250 seeders,       250 leechers:	          13536B (        13KiB)
      500 seeders,       500 leechers:	          26096B (        25KiB)
     1000 seeders,      1000 leechers:	          50320B (        49KiB)
     2500 seeders,      2500 leechers:	         183216B (       178KiB)
     5000 seeders,      5000 leechers:	         352080B (       343KiB)
    10000 seeders,     10000 leechers:	         712528B (       695KiB)
    25000 seeders,     25000 leechers:	        1581904B (      1544KiB,      1.5MiB)
    50000 seeders,     50000 leechers:	        3158768B (      3084KiB,      3.0MiB)
   100000 seeders,    100000 leechers:	        6319616B (      6171KiB,      6.0MiB)
   250000 seeders,    250000 leechers:	       12655744B (     12359KiB,     12.1MiB)
   500000 seeders,    500000 leechers:	       25302192B (     24709KiB,     24.1MiB)
  1000000 seeders,   1000000 leechers:	       50605152B (     49419KiB,     48.3MiB)
  2500000 seeders,   2500000 leechers:	      170454192B (    166459KiB,    162.6MiB)
  5000000 seeders,   5000000 leechers:	      340471120B (    332491KiB,    324.7MiB)
 10000000 seeders,  10000000 leechers:	      681634512B (    665658KiB,    650.1MiB)
 25000000 seeders,  25000000 leechers:	     1616537632B (   1578650KiB,   1541.7MiB, 1.5GiB)
```

Note that there are no differences between IPv4 and IPv6 peers regarding memory usage.

Here's a benchmark comparison between the chihaya `memory` implementation (old) and this implementation (new).
The benchmarks were done on go 1.9, September 2 2017:

```
benchmark                                 old ns/op     new ns/op     delta
BenchmarkNop                              4.66          4.61          -1.07%
BenchmarkNop-4                            1.19          1.20          +0.84%
BenchmarkPut                              415           352           -15.18%
BenchmarkPut-4                            531           483           -9.04%
BenchmarkPut1k                            463           526           +13.61%
BenchmarkPut1k-4                          588           778           +32.31%
BenchmarkPut1kInfohash                    457           377           -17.51%
BenchmarkPut1kInfohash-4                  144           133           -7.64%
BenchmarkPut1kInfohash1k                  465           375           -19.35%
BenchmarkPut1kInfohash1k-4                140           138           -1.43%
BenchmarkPutDelete                        1455          1176          -19.18%
BenchmarkPutDelete-4                      1795          1821          +1.45%
BenchmarkPutDelete1k                      1469          1099          -25.19%
BenchmarkPutDelete1k-4                    1946          2096          +7.71%
BenchmarkPutDelete1kInfohash              1518          1179          -22.33%
BenchmarkPutDelete1kInfohash-4            2033          2077          +2.16%
BenchmarkPutDelete1kInfohash1k            1606          1141          -28.95%
BenchmarkPutDelete1kInfohash1k-4          1803          1814          +0.61%
BenchmarkDeleteNonexist                   221           324           +46.61%
BenchmarkDeleteNonexist-4                 194           405           +108.76%
BenchmarkDeleteNonexist1k                 220           350           +59.09%
BenchmarkDeleteNonexist1k-4               192           413           +115.10%
BenchmarkDeleteNonexist1kInfohash         242           349           +44.21%
BenchmarkDeleteNonexist1kInfohash-4       77.2          116           +50.26%
BenchmarkDeleteNonexist1kInfohash1k       235           368           +56.60%
BenchmarkDeleteNonexist1kInfohash1k-4     78.1          117           +49.81%
BenchmarkPutGradDelete                    2299          1524          -33.71%
BenchmarkPutGradDelete-4                  3025          2494          -17.55%
BenchmarkPutGradDelete1k                  2158          1528          -29.19%
BenchmarkPutGradDelete1k-4                3075          2296          -25.33%
BenchmarkPutGradDelete1kInfohash          2449          1566          -36.06%
BenchmarkPutGradDelete1kInfohash-4        3209          2001          -37.64%
BenchmarkPutGradDelete1kInfohash1k        2406          1602          -33.42%
BenchmarkPutGradDelete1kInfohash1k-4      3010          2775          -7.81%
BenchmarkGradNonexist                     448           352           -21.43%
BenchmarkGradNonexist-4                   584           484           -17.12%
BenchmarkGradNonexist1k                   482           544           +12.86%
BenchmarkGradNonexist1k-4                 722           764           +5.82%
BenchmarkGradNonexist1kInfohash           498           377           -24.30%
BenchmarkGradNonexist1kInfohash-4         156           138           -11.54%
BenchmarkGradNonexist1kInfohash1k         511           435           -14.87%
BenchmarkGradNonexist1kInfohash1k-4       160           137           -14.38%
BenchmarkAnnounceLeecher                  17847         6086          -65.90%
BenchmarkAnnounceLeecher-4                5818          1995          -65.71%
BenchmarkAnnounceLeecher1kInfohash        22159         10535         -52.46%
BenchmarkAnnounceLeecher1kInfohash-4      7143          3084          -56.82%
BenchmarkAnnounceSeeder                   18329         6738          -63.24%
BenchmarkAnnounceSeeder-4                 5530          2268          -58.99%
BenchmarkAnnounceSeeder1kInfohash         23418         11355         -51.51%
BenchmarkAnnounceSeeder1kInfohash-4       7354          3159          -57.04%
BenchmarkScrapeSwarm                      84.2          90.1          +7.01%
BenchmarkScrapeSwarm-4                    52.5          52.2          -0.57%
BenchmarkScrapeSwarm1kInfohash            116           119           +2.59%
BenchmarkScrapeSwarm1kInfohash-4          34.5          40.0          +15.94%

benchmark                                 old allocs     new allocs     delta
BenchmarkNop                              0              0              +0.00%
BenchmarkNop-4                            0              0              +0.00%
BenchmarkPut                              2              1              -50.00%
BenchmarkPut-4                            2              1              -50.00%
BenchmarkPut1k                            2              1              -50.00%
BenchmarkPut1k-4                          2              1              -50.00%
BenchmarkPut1kInfohash                    2              1              -50.00%
BenchmarkPut1kInfohash-4                  2              1              -50.00%
BenchmarkPut1kInfohash1k                  2              1              -50.00%
BenchmarkPut1kInfohash1k-4                2              1              -50.00%
BenchmarkPutDelete                        6              5              -16.67%
BenchmarkPutDelete-4                      6              5              -16.67%
BenchmarkPutDelete1k                      6              5              -16.67%
BenchmarkPutDelete1k-4                    6              5              -16.67%
BenchmarkPutDelete1kInfohash              6              5              -16.67%
BenchmarkPutDelete1kInfohash-4            6              5              -16.67%
BenchmarkPutDelete1kInfohash1k            6              5              -16.67%
BenchmarkPutDelete1kInfohash1k-4          6              5              -16.67%
BenchmarkDeleteNonexist                   2              2              +0.00%
BenchmarkDeleteNonexist-4                 2              2              +0.00%
BenchmarkDeleteNonexist1k                 2              2              +0.00%
BenchmarkDeleteNonexist1k-4               2              2              +0.00%
BenchmarkDeleteNonexist1kInfohash         2              2              +0.00%
BenchmarkDeleteNonexist1kInfohash-4       2              2              +0.00%
BenchmarkDeleteNonexist1kInfohash1k       2              2              +0.00%
BenchmarkDeleteNonexist1kInfohash1k-4     2              2              +0.00%
BenchmarkPutGradDelete                    9              6              -33.33%
BenchmarkPutGradDelete-4                  9              6              -33.33%
BenchmarkPutGradDelete1k                  9              6              -33.33%
BenchmarkPutGradDelete1k-4                9              6              -33.33%
BenchmarkPutGradDelete1kInfohash          9              6              -33.33%
BenchmarkPutGradDelete1kInfohash-4        9              6              -33.33%
BenchmarkPutGradDelete1kInfohash1k        9              6              -33.33%
BenchmarkPutGradDelete1kInfohash1k-4      9              6              -33.33%
BenchmarkGradNonexist                     2              1              -50.00%
BenchmarkGradNonexist-4                   2              1              -50.00%
BenchmarkGradNonexist1k                   2              1              -50.00%
BenchmarkGradNonexist1k-4                 2              1              -50.00%
BenchmarkGradNonexist1kInfohash           2              1              -50.00%
BenchmarkGradNonexist1kInfohash-4         2              1              -50.00%
BenchmarkGradNonexist1kInfohash1k         2              1              -50.00%
BenchmarkGradNonexist1kInfohash1k-4       2              1              -50.00%
BenchmarkAnnounceLeecher                  57             52             -8.77%
BenchmarkAnnounceLeecher-4                57             52             -8.77%
BenchmarkAnnounceLeecher1kInfohash        57             52             -8.77%
BenchmarkAnnounceLeecher1kInfohash-4      57             52             -8.77%
BenchmarkAnnounceSeeder                   57             52             -8.77%
BenchmarkAnnounceSeeder-4                 57             52             -8.77%
BenchmarkAnnounceSeeder1kInfohash         57             52             -8.77%
BenchmarkAnnounceSeeder1kInfohash-4       57             52             -8.77%
BenchmarkScrapeSwarm                      0              0              +0.00%
BenchmarkScrapeSwarm-4                    0              0              +0.00%
BenchmarkScrapeSwarm1kInfohash            0              0              +0.00%
BenchmarkScrapeSwarm1kInfohash-4          0              0              +0.00%

benchmark                                 old bytes     new bytes     delta
BenchmarkNop                              0             0             +0.00%
BenchmarkNop-4                            0             0             +0.00%
BenchmarkPut                              64            32            -50.00%
BenchmarkPut-4                            64            32            -50.00%
BenchmarkPut1k                            64            32            -50.00%
BenchmarkPut1k-4                          64            32            -50.00%
BenchmarkPut1kInfohash                    64            32            -50.00%
BenchmarkPut1kInfohash-4                  64            32            -50.00%
BenchmarkPut1kInfohash1k                  64            32            -50.00%
BenchmarkPut1kInfohash1k-4                64            32            -50.00%
BenchmarkPutDelete                        400           176           -56.00%
BenchmarkPutDelete-4                      400           176           -56.00%
BenchmarkPutDelete1k                      400           176           -56.00%
BenchmarkPutDelete1k-4                    400           176           -56.00%
BenchmarkPutDelete1kInfohash              400           176           -56.00%
BenchmarkPutDelete1kInfohash-4            400           176           -56.00%
BenchmarkPutDelete1kInfohash1k            400           176           -56.00%
BenchmarkPutDelete1kInfohash1k-4          400           176           -56.00%
BenchmarkDeleteNonexist                   48            48            +0.00%
BenchmarkDeleteNonexist-4                 48            48            +0.00%
BenchmarkDeleteNonexist1k                 48            48            +0.00%
BenchmarkDeleteNonexist1k-4               48            48            +0.00%
BenchmarkDeleteNonexist1kInfohash         48            48            +0.00%
BenchmarkDeleteNonexist1kInfohash-4       48            48            +0.00%
BenchmarkDeleteNonexist1kInfohash1k       48            48            +0.00%
BenchmarkDeleteNonexist1kInfohash1k-4     48            48            +0.00%
BenchmarkPutGradDelete                    672           208           -69.05%
BenchmarkPutGradDelete-4                  672           208           -69.05%
BenchmarkPutGradDelete1k                  672           208           -69.05%
BenchmarkPutGradDelete1k-4                672           208           -69.05%
BenchmarkPutGradDelete1kInfohash          672           208           -69.05%
BenchmarkPutGradDelete1kInfohash-4        672           208           -69.05%
BenchmarkPutGradDelete1kInfohash1k        672           208           -69.05%
BenchmarkPutGradDelete1kInfohash1k-4      672           208           -69.05%
BenchmarkGradNonexist                     64            32            -50.00%
BenchmarkGradNonexist-4                   64            32            -50.00%
BenchmarkGradNonexist1k                   64            32            -50.00%
BenchmarkGradNonexist1k-4                 64            32            -50.00%
BenchmarkGradNonexist1kInfohash           64            32            -50.00%
BenchmarkGradNonexist1kInfohash-4         64            32            -50.00%
BenchmarkGradNonexist1kInfohash1k         64            32            -50.00%
BenchmarkGradNonexist1kInfohash1k-4       64            32            -50.00%
BenchmarkAnnounceLeecher                  8528          4552          -46.62%
BenchmarkAnnounceLeecher-4                8528          4552          -46.62%
BenchmarkAnnounceLeecher1kInfohash        8528          4552          -46.62%
BenchmarkAnnounceLeecher1kInfohash-4      8528          4552          -46.62%
BenchmarkAnnounceSeeder                   8528          4552          -46.62%
BenchmarkAnnounceSeeder-4                 8528          4552          -46.62%
BenchmarkAnnounceSeeder1kInfohash         8528          4552          -46.62%
BenchmarkAnnounceSeeder1kInfohash-4       8528          4552          -46.62%
BenchmarkScrapeSwarm                      0             0             +0.00%
BenchmarkScrapeSwarm-4                    0             0             +0.00%
BenchmarkScrapeSwarm1kInfohash            0             0             +0.00%
BenchmarkScrapeSwarm1kInfohash-4          0             0             +0.00%
```

Note that these are _just benchmarks_, not real-world metrics.

## License
MIT, see the LICENSE file
