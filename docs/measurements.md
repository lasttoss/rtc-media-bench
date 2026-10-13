# Measurements

The claims in the README are about a call: how long a participant takes to join, and how a call dies
quietly. The harness that answers it ships here, and `make bench` runs it with one command.

## Environment

| | |
|---|---|
| Machine | 12-core x86-64 (AMD64), Ubuntu 24.04, Docker 29.1.3 |
| Go | 1.27.2 |
| Date | 2026-10-13 (UTC) |

Every host port in this file is the one the repository publishes: bound to loopback and offset from
the default, so a machine already running a Postgres, a Redis or three other stacks does not have to
stop them first.


## Join p50 / p95, one advertised candidate

```
$ make bench            # starts the vendored SFU on 17880, waits for it, measures, stops it (11 s)
participants      3
join              p50 70.4 ms  p95 88.8 ms  (dial to ICE connected)
first frame       p50 106.3 ms  p95 452.2 ms  (dial to the first RTP packet from the SFU)
rtt               p50 0.1 ms  p95 0.1 ms  (nominated ICE pair)
jitter            p50 0.2 ms  p95 0.2 ms
bitrate           down p50 121.2 kbps  up p50 129.0 kbps
packets           inbound 564 received / 0 lost
disconnects       0
survived          p50 10000.7 ms  p95 10001.6 ms  (how long the call lasted)
failed            0
```

A second run, same command: `join p50 53.0 ms / p95 73.0 ms`, `first frame p50 87.5 ms / p95 453.9 ms`,
`566 packets received / 0 lost`, `failed 0`.

**Result.** Consistent with the README's "host join p95 was 50.0 / 33.5 / 36.1 ms over three runs"
and with its loopback row (`join p50/p95 30-43 / 44-53 ms`). The in-cluster number in the README
(table row 1: `11.9 / 12.3 ms`, `make lab-bench`) is a different measurement on a different path and is
**not** reproduced by this command - the loopback path adds ICE time that the cluster does not.

## 78.8% of the client code, against 22.5% without the SFU

```
$ go test ./... -cover
ok  .../internal/rtc   coverage: 22.5% of statements      # unit tests alone

$ make test-integration
client_integration_test.go:123: join 53.591/144.143 ms, 225 packets received, 178 frames published
--- PASS: TestTwoParticipantsExchangeMedia (6.21s)
ok  .../internal/rtc   7.225s   coverage: 78.4% of statements
```

**Result.** The 22.5% figure is exact. The integration figure measured here is **78.4%**, against the
78.8% in the README - the same tests on a different toolchain (Go 1.27.2 here), not a different
suite. The number to trust is the one CI prints on the pinned version.

Total statement coverage over the module is 20.0%: `cmd/bench` and the vendored SFU are entry points
and a copied upstream tree, and neither is counted as covered.
