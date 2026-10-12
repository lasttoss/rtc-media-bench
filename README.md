# rtc-media-bench

[![ci](https://github.com/lasttoss/rtc-media-bench/actions/workflows/ci.yml/badge.svg)](https://github.com/lasttoss/rtc-media-bench/actions/workflows/ci.yml)

A WebRTC call is either connected or it is not, and almost everything that reports on it - a
websocket that answers, a pod that says Ready, a readiness probe that returns in microseconds - says
nothing about whether media is arriving. This repository runs an SFU in Kubernetes, measures what a
participant can actually see, and reproduces the faults that make a call fail while every dashboard
stays green.

One of those faults turned out not to reproduce at all. That is written down with the four
configurations it was tried in, because a fault catalogue with no gaps in it is a catalogue somebody
did not test.

```
make demo         # no cluster, no credentials: build, measure on loopback, validate the chart
make lab-up       # kind + Calico + both images + the chart
make lab-bench    # run the harness against the cluster, through the node port
make measure      # the ICE comparison, into lab/evidence/
make verify-sfu   # assert the vendor patch reaches the peer connection
make test-integration  # two real participants through the real SFU, as a test
make verify-patch # re-derive the vendored SFU from upstream and diff it
```

## What this demonstrates

| capability | how it is done here | where |
|---|---|---|
| **a real WebRTC peer, not a simulation** | headless participants publish real VP8 frames through pion and measure what their own peer connection reports: ICE timings, the nominated pair's RTT, packets received, bytes on the wire | `internal/rtc`, `cmd/bench` |
| **timings split so a fault has an address** | `dial`, `offer` and `ice` are reported separately, because a slow websocket, a slow SFU and an unreachable candidate are three different problems | `internal/rtc/client.go`, `internal/report` |
| **an SFU that is configured, not assumed** | upstream pion `sfu-ws` vendored at a pinned commit with one ICE patch; `make verify-patch` re-downloads upstream and fails unless the result matches byte for byte | `lab/sfu`, `lab/verify-patch.sh` |
| **a chart whose broken configuration is still tested** | media through a Service is a deliberate chart mode; every branch is rendered and validated in CI, so the broken one cannot rot | `charts/sfu`, `lab/chart-check.sh` |
| **measurements that refuse to lie** | `lab/run-cluster.sh` will not run unless the pod is the image just built, has not restarted, has an endpoint, and the port answers; missing send-side loss prints as `n/a`, never as `0` | `lab/run-cluster.sh`, `internal/report` |
| **a fault catalogue with its gaps named** | four faults reproduced or explained, one not reproduced with the evidence of four attempts, plus the fault nobody writes down | `docs/runbook.md` |

## The numbers, with their provenance

| measurement | value | how it was produced |
|---|---|---|
| join p50 / p95, one advertised candidate | **11.9 / 12.3 ms** | kind cluster, 3 participants, `make lab-bench` |
| join p50 / p95, one address, loopback | 30-43 / 44-53 ms | two runs, 3 participants, `make measure` |
| join p50 / p95, every interface advertised | 160-427 / **315-434 ms** | two runs, 3 participants, `make measure` |
| host candidates offered, per case | **1 vs 20** | `advertised` from the nominated pair's stats |
| SFU CPU per participant | **50.7 ms = 1.0 % of one core** | cgroup `cpu.stat` of the pod, 3 participants, 15.8 s |
| packets received, 3 participants, 8 s | 436 | `inbound_packets` |
| send-side packet loss | **n/a** | RTCP receiver reports did not populate here |

Loopback describes loopback and one kind node describes one laptop. Both are reproducible with the
commands above, and neither is a capacity claim.

**One outlier, recorded rather than explained:** in one cluster run a participant took 9.2 s to join
where the other two took 15 ms. Three later runs with the same configuration did not repeat it and the
mechanism was not identified.

**A second one, explained and fixed:** `TestTwoParticipantsExchangeMedia` failed every few runs on a
loaded machine - `participant 0 never reached ICE connected`, then `close 1006` - and `-race` reported a
data race in `internal/rtc/client.go`, where `Run` returned its `Result` while pion's ICE callbacks were
still appending to it. The same code sent candidates as soon as `SetLocalDescription` began gathering,
before the answer that explains them, and the SFU rejected them with `Failed to add ICE candidate:
remote description is not set`. Both are fixed: the fields a callback owns are written under one lock and
copied into the result once, and candidates wait for the answer. The loop that found it is the CI command
run twelve times on a loaded machine - five failures before, none in the twenty runs since. The Makefile
target that reported those failures as successes (`go test` piped into `grep`/`sed`, whose exit status
won) sets `pipefail` now.

## The fault catalogue

Full entries, with symptoms, checks and evidence, are in **[`docs/runbook.md`](docs/runbook.md)**.

| # | fault | measured outcome |
|---|---|---|
| 1 | egress policy does not let the pod answer | **not reproduced** - four configurations tried, including Calico and a client inside the cluster; the replies to a peer's checks are established traffic and are allowed. Written up as a gap with the reason |
| 2 | media exposed through a Service | **reproduced**: advertised `172.19.0.2:7882`, listening on node port `30782`, `join n=0`, no media, **and no error anywhere** |
| 3 | the SFU advertises the pod's own address | **reproduced**: candidate `10.244.0.17:7882`, `join n=0`, `failed 0` - a fault that fails silently |
| 4 | pod deleted with and without a grace period | **reproduced**: calls survived 8.2-9.1 s **both times**; the grace period changed nothing to within 50 ms, because a Go program with no `SIGTERM` handler exits on it immediately |
| 5 | advertising every interface | 20 host candidates, join p95 **315-434 ms** against **44-53 ms** for one |

## How it works

**Figure 1.** Participants signal over a websocket through a Service and send media to the address the
SFU advertises. With `hostPort` exposure and one multiplexed UDP port the SFU offers exactly one
candidate - the node's address on port 7882 - and the call connects in about twelve milliseconds. When
media is exposed through a Service the advertised port and the listening port differ, and the call
never starts while everything still reports healthy. The fault catalogue sits underneath, as measured.

![rtc-media-bench: participants signal through a Service and send media to the address the SFU advertises; with hostPort and a single multiplexed UDP port one candidate is offered and joins take about twelve milliseconds, while exposing media through a Service makes the advertised and listening ports differ so no participant joins and no error is reported; the fault catalogue below lists an egress policy that did not reproduce, a translated media port that did, a pod advertising its own address that did, a pod deletion with and without a grace period that behaved identically, and advertising every interface costing 969 milliseconds at p95 against 18](docs/figures/rtc-media-bench-flow.png)

Generated by a committed script that measures its own text before placing a box
(`docs/figures/rtc-media-bench-flow.generator.py`); `make diagram` fails when the export is older than
the sources named in its `diagram-meta`.

The chart's most important value is not a resource limit - it is how media leaves the pod:

| exposure | signalling | media | honest verdict |
|---|---|---|---|
| `hostPort` (default) | Service | node port = container port | works: measured 11.9 ms p50 |
| `hostNetwork` | Service | node network | works; the pod sees every address the node has |
| `nodePort` | Service | Service translates the port | **does not work**, and this is fault 2: ICE tells the peer the container port |

## What this does not claim

- **Not a media-quality benchmark.** No codec work, no echo cancellation, no MOS, no TURN. It measures
  reachability, timing, addresses and packet counts through a real stack.
- **Not a fork of an SFU.** The SFU is upstream pion `sfu-ws` with a small ICE patch; the media path is
  untouched and `PROVENANCE.md` says exactly what was changed and why.
- **One node, one replica.** Multi-node scheduling and the room loss that comes with it is out of scope.
- **The fault catalogue has a hole in it**, named in the runbook: fault 1 did not reproduce in this
  environment, and the runbook explains where it would.
- **Send-side loss was not measured**, because the receiver reports that carry it did not appear in
  this setup. The field prints `n/a`. A number that reads as a perfect network is worse than no number.

## Repository standard

| item | here | note |
|---|---|---|
| `docker-compose.yml` | n/a | this is a cluster lab: `make lab-up` is the one-command path, and `make demo` runs without a cluster at all |
| `Dockerfile` | ✅ | two of them, both distroless nonroot: the SFU built from the vendored source, and the harness as an image so a load test can run inside the cluster |
| Helm chart | ✅ | `charts/sfu`: Deployment, Service, NetworkPolicy on by default; every branch rendered and validated in CI |
| `docs/figures/` + freshness gate | ✅ | `rtc-media-bench-flow` with `diagram-meta`; `make diagram` fails on a stale export |
| tests + coverage in CI | ✅ | unit tests need no cluster, no network and no credentials; `make test-integration` puts two real participants through the vendored SFU (78.8% of the client code, against 22.5% without it), and `lab/check-ice-config.sh` fails unless the ICE flags reach the peer connection |
| README with what it demonstrates | ✅ | this file, with the numbers and their provenance |

## Licence

MIT, see `LICENSE`. The vendored SFU keeps its own MIT licence in `lab/sfu/LICENSE`. What came from
where is in `PROVENANCE.md`.
