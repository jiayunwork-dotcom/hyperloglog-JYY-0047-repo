# hyperloglog

A Go library and command line tool for estimating stream statistics in sublinear
memory. It reads a list of elements (one per line) and answers two kinds of
question without ever holding the whole stream: **how many distinct elements
were there** (HyperLogLog) and **how often did a given element occur, and which
elements dominate the stream** (Count-Min sketch). Input is line-oriented UTF-8
text on standard input; output is plain text on standard output — a single
integer for a point query, or one row per element for a heavy-hitter listing.
Memory is bounded by the sketch shape rather than by the number of elements: a
default HyperLogLog occupies 16 KiB regardless of whether the stream carries a
thousand or a billion distinct values. The estimates are approximate by
construction — distinct counts carry a relative error near 0.8% at the default
precision, and frequency estimates are one-sided upper bounds that never fall
below the true count. Exact answers are out of scope; so is any form of
persistence or networking.

## Why sketches

Counting distinct elements exactly needs memory proportional to the number of
distinct elements. Counting occurrences exactly needs a full frequency table.
Both become impractical on large streams. The two structures here trade a
bounded, quantifiable amount of accuracy for a fixed memory footprint, and both
are *mergeable*: partial sketches computed over shards of a stream can be
combined into a sketch of the whole, which is what makes them useful for
distributed aggregation.

## Install and build

```bash
go build ./...
go test ./...
go vet ./...
```

The module needs Go 1.21 and depends only on the standard library.

## Command line usage

```
hyperloglog card [-p precision] [-v]
hyperloglog cms  [-d depth] [-w width] <item>
hyperloglog topk [-d depth] [-w width] [-n limit] <phi>
```

Elements are read from standard input, one per line. Surrounding whitespace is
trimmed and blank lines are skipped.

### `card` — how many distinct elements

```bash
$ hyperloglog card < example/items.txt
1188
```

The file holds 5000 lines covering 1180 distinct identifiers, so the estimate is
off by about 0.7%. `-p` sets the precision (the sketch keeps `2^p` registers,
4 ≤ p ≤ 18); `-v` adds a diagnostic report:

```bash
$ hyperloglog card -v < example/items.txt
lines           5000
distinct        1188
precision       14
registers       16384
representation  sparse
occupied        1146
max register    9
standard error  0.8125%
```

### `cms` — how often did one element occur

```bash
$ hyperloglog cms /login < example/access.txt
260
```

The estimate is an upper bound: it is never below the true count, and it
overshoots only when the element collides with others in every row of the
matrix. `-d` and `-w` set the number of counter rows and the counters per row;
widening the matrix reduces the overshoot.

### `topk` — which elements dominate

```bash
$ hyperloglog topk 0.1 < example/access.txt
/                                       400  31.873%
/login                                  260  20.717%
/search                                 150  11.952%
```

`phi` is a fraction in `[0,1]`; elements whose estimated share of the stream
strictly exceeds it are listed, heaviest first. `-n` caps how many rows print.

Flags may be written before or after the positional argument — `cms -w 4096 x`
and `cms x -w 4096` behave identically. Use `--` before an element that itself
starts with a dash.

## Packages

### `internal/hash`

The 64-bit hashing primitive the sketches share: FNV-1a followed by an
avalanche finalisation step. The finalisation matters because HyperLogLog reads
the register index from the *leading* bits of the digest, where plain FNV-1a
diffuses poorly.

- `Hash64`, `Hash64String`, `Seeded`, `DeriveSeed`, `Mix64`
- `Digest` — streaming form implementing `io.Writer`; chunked writes and a
  one-shot call agree.
- `Split(h, p)` — decomposes a digest into the register index (the top `p`
  bits) and `rho`, the one-based position of the leftmost set bit in the
  remaining suffix. The two results come from disjoint bit ranges, which is
  what makes them independent.
- `Encode`/`Decode` — pack an `(index, rho)` pair into one 32-bit word.

### `internal/hll`

HyperLogLog. `m = 2^p` single-byte registers; each element raises one register
to the run length it produces, never lowers it, so the sketch is order
independent and idempotent.

- Two representations. A new sketch stores only the registers actually touched,
  in a map keyed by register index, so memory tracks the observed cardinality
  and small counts come out essentially exact. Once occupancy passes
  `SparseLimit` the sketch promotes itself to the flat `m`-byte array.
  Promotion is invisible to callers: the estimator reads register *values*, and
  both representations hold the same ones, so `Count` does not move across the
  transition.
- `Count` uses the harmonic-mean estimator scaled by `alpha_m`, falling back to
  linear counting over the empty registers in the small-cardinality regime
  where the raw estimator is badly biased.
- `Merge` takes the register-wise maximum, giving a sketch of the union that is
  identical to one built from both streams in a single pass.
- `MarshalBinary`/`UnmarshalBinary` preserve the representation and produce a
  canonical encoding, so equal sketches serialise to equal bytes. A rejected
  decode leaves the receiver untouched.

### `internal/cms`

Count-Min sketch. A `d`-by-`w` matrix of counters plus `d` derived hash
functions; adding an element increments one counter per row and `Estimate`
returns the smallest of them.

- Estimates are one-sided: every counter an element touches holds at least its
  true count, and collisions only inflate counters, so the minimum is the
  tightest available upper bound and never an undercount.
- `Merge` adds counters, which is exact because a counter is a sum of
  increments and both sketches share the same hash functions.
- `HeavyHitters(phi)` returns the elements whose estimated share strictly
  exceeds `phi`, ordered by descending estimate with ties broken by element, so
  the ordering is total and reproducible. `TopK(k)` is the fixed-count variant.
  A bare Count-Min sketch cannot enumerate what it has seen, so the distinct
  element keys are recorded alongside the matrix to make these queries
  possible.
- `NewWithAccuracy(epsilon, delta)` sizes the matrix from a target overshoot
  and confidence rather than from raw dimensions.

## Accuracy

| precision `p` | registers | dense size | relative standard error |
|---|---|---|---|
| 10 | 1024 | 1 KiB | 3.25% |
| 12 | 4096 | 4 KiB | 1.63% |
| 14 | 16384 | 16 KiB | 0.81% |
| 16 | 65536 | 64 KiB | 0.41% |

For Count-Min, a `d`-by-`w` matrix keeps the additive overshoot on a point
query below `e/w` times the stream total with probability at least `1 - e^-d`;
`ErrorBound` and `Confidence` report both numbers for a live sketch.

## Error handling

The library returns errors rather than terminating: out-of-range precisions and
matrix shapes, empty elements, mismatched shapes on merge, counter overflow,
and malformed serialised sketches all surface as values that wrap a documented
sentinel, so callers can match them with `errors.Is`. Only the command line
front end exits non-zero.

## Example data

- `example/items.txt` — 5000 lines over 1180 distinct identifiers, for `card`.
- `example/access.txt` — 1255 request paths with a deliberately skewed
  distribution, for `cms` and `topk`.

## License

MIT. See `LICENSE`.
