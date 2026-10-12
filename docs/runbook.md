# Runbook: a call that does not connect

Written from what this lab measured, on a kind cluster and on loopback, with the numbers in place.
Every entry names the symptom, how to tell it apart from the others, the evidence, the fix, and what
was actually observed here - including one fault that **did not reproduce**, which is the most useful
entry in the file.

The theme is the same throughout: **signalling and media fail differently.** A websocket that answers
and a pod that reports Ready say nothing about whether RTP will arrive, and the tools that report
"healthy" are the tools that make this class of fault expensive.

## How to run the lab

```
kind create cluster --name rtc-bench
docker build -t rtc-media-bench/sfu:local -f lab/sfu/Dockerfile .
docker build -t rtc-media-bench/bench:local -f lab/bench/Dockerfile .
make lab-up           # kind cluster + Calico + both images loaded + chart installed
lab/faults/02-media-through-a-service.sh   # any fault script: apply, measure, fix, measure again
make lab-down
```

`lab/run-cluster.sh <label>` is the only path to a measurement. It refuses to run unless the pod is
the image that was just built, the pod has not restarted, the Service has an endpoint, and the
signalling port actually answers. Each of those checks exists because one run was taken without it and
described the wrong system.

## What a reading means

`join` is split into `dial` (websocket), `offer` (first SDP from the SFU) and `ice` (the moment ICE
found a path). They are kept apart because they fail for different reasons and a single number hides
which one happened:

- `dial` slow or refused: the Service, the node port, or the pod is not up.
- `offer` slow: the SFU is up and the signalling path is working; look at the SFU, not the network.
- `ice` slow or never: addresses, ports and policies. **This is where every fault below lives.**

`advertised` is the candidate list the peer actually offered, read from the nominated pair's stats.

---

## 1. Media never arrives, pod is Ready, every probe passes (egress policy) - NOT REPRODUCED

**Symptom.** Signalling connects, ICE never completes, no participant receives anything, and the pod
reports `1/1 Running` with a readiness probe that answers in microseconds.

**Why it is the expensive one.** Nothing in the pod is broken. The pod is not allowed to *answer*: ICE
sends connectivity checks to addresses it never addressed first, and a policy written like an API
allow-list - DNS plus the services this pod calls - drops them.

**How to check.**

```
kubectl -n <ns> get networkpolicy <release> -o jsonpath='{.spec.egress}'   # is there a rule that is not DNS?
kubectl -n <ns> exec <another pod> -- nc -vz -u <pod-ip> <mux-port>        # from a pod, not from the node
```

**The fix.** Allow egress to the peer network on the media port, as `charts/sfu/templates/networkpolicy.yaml`
does by default. Egress to peers is a first-class part of a media policy, not an addition.

**What was measured here.** Four configurations: kind's default CNI, then Calico; client on the host,
then client as a pod in the same namespace. With the egress rule closed, the call still connected and
media still flowed in all four (`join p50 5.5 ms`, 148 packets inbound).

**Why it did not reproduce, as far as this lab can tell.** The client's own checks reach the SFU, and
the SFU's *replies* to those checks are traffic in an established flow, which the policy allows. The
fault needs a topology where the return path is itself a new flow - a NAT, a policy that also blocks
established traffic, or a peer that only answers when the server initiates. That is where this lab
stops and says so rather than claiming a reproduction it does not have.

## 2. Media through a Service: signalling works, the port does not match - REPRODUCED

**Symptom.** The websocket connects, the pod is Ready, ICE never completes, `join` is empty and no
error is reported. From the outside the call simply does not start.

**Why.** ICE tells the peer which **address and port** to send to. A Service translates ports: the SFU
is configured to send `host:7882`, the Service listens on `host:30782`, and nothing is listening on
the port every peer has been told to use. This is what a default Service-based deployment looks like.

**Evidence measured here** (`make fault2`):

```
advertised  172.19.0.2:7882/host      <- what the SFU tells peers
service     NodePort 30080, 30782, 30783   <- what actually listens
result      join n=0, inbound 0, signalling fine, no errors
```

**The fix.** Expose media with `hostPort` (or `hostNetwork`) so the port on the node is the port ICE
advertises: `exposure: hostPort` in the chart. After the fix, the same call measured
`join p50 10.7 ms`, 439 packets received.

**How to check in a live cluster.**

```
kubectl -n <ns> get svc <release> -o jsonpath='{.spec.ports}'    # does any media port have a nodePort?
kubectl get pod <sfu-pod> -o jsonpath='{.spec.containers[0].ports}'  # is there a hostPort?
```

## 3. The SFU advertises the address it thinks it has (pod IP) - REPRODUCED

**Symptom.** No participant joins, and **nothing reports an error**. The harness shows
`join n=0, inbound 0, failed 0`; the pod is Ready; signalling is fine.

**Why.** Inside a pod, the address the process sees is a pod CIDR address, reachable by nothing
outside the cluster. A candidate nobody can reach is not an error to ICE - it is a candidate that
never succeeds, and something else may still work. On one host it does: measured locally, an
unroutable advertised address still connected in 18 ms over an IPv6 link-local candidate nobody asked
for. A cluster has no such escape route.

**Evidence measured here** (`make fault3`):

```
advertised  10.244.0.17:7882/host     <- the pod's own address
result      join n=0, inbound 0, failed 0, pod Ready
after fix   advertised 172.19.0.2:7882/host, join p50 12.1 ms, 439 packets
```

**The fix.** `advertise.strategy: nodeIP` - the chart injects `status.hostIP` through the downward API
and passes it to `-advertise-ip`. A NAT or a load balancer in front changes this to `explicit`.

**How to check.**

```
kubectl -n <ns> get pod <sfu-pod> -o jsonpath='{.spec.containers[0].args}'   # what is advertised?
kubectl -n <ns> logs <sfu-pod> | grep -i advertise                          # usually nothing: see below
```

## 4. The pod goes away, with and without a grace period - REPRODUCED, and the grace bought nothing

**Symptom.** Every participant loses the call at the same instant; the pod is replaced and clients
reconnect.

**Evidence measured here** (`lab/faults/04-restart-without-grace.sh`): a call of three participants,
pod force-deleted nine seconds in, run twice - once with `terminationGracePeriodSeconds: 0`, once with
30:

| grace | join p50 | inbound packets | call survived | outcome |
|---|---|---|---|---|
| 0 s | 10.3 ms | 987 | 8.2, 8.7, 9.1 s | websocket closed on all three |
| 30 s | 12.1 ms | 987 | 8.3, 8.7, 9.1 s | websocket closed on all three |

**The finding.** The grace period changed nothing, to within 50 ms. `SIGTERM` is sent at the start of
the grace period, and a Go program with no signal handler exits on it immediately - so the container
stops at once and the thirty seconds are spent waiting for a process that is already gone. **A grace
period is only as good as the handler that uses it.**

**The fix.** Trap `SIGTERM` in the SFU, stop accepting new rooms, and let existing ones drain before
exiting: then the grace period buys exactly the drain, which is what it is for. The harness reports
`survived_ms` so the drain can be measured rather than assumed.

## 5. The fault that is not in anyone's runbook: advertising every interface

**Symptom.** Calls connect, sometimes, and take a second or two. Nothing is broken and nothing is
fast.

**Why.** ICE gathers a candidate per interface. A host with docker bridges, veth pairs and link-local
IPv6 addresses offers dozens, most of them unreachable from the peer. Every one costs a connectivity
check that fails before the working candidate is tried.

**Evidence measured here** (loopback, 3 participants, `make measure`):

| advertised | join p50 | join p95 | candidates offered |
|---|---|---|---|
| every interface (pion's default) | 149 ms | **969 ms** | 45 |
| one address (`-advertise-ip`) | 16.8 ms | **18.3 ms** | 3 |

A third run in the cluster had one participant take 9.2 s where the other two took 15 ms; the same
run configuration did not do it again in three later attempts. Recorded as an outlier with no
identified mechanism rather than explained away.

**The fix.** Advertise the address peers can actually reach, and keep the candidate list short.

## 6. The pod is Running and restarting: a mux bind per interface

**Symptom.** The pod shows `Running`, the readiness probe answers for a few seconds at a time, and
`RESTARTS` climbs. Calls connect only in the windows when the container happens to be up.

**Evidence.** `kubectl -n rtc logs <sfu-pod> --previous`:

```
Could not configure ICE: udp mux on 7882: listen udp [fe80::7c89:39ff:fede:d22%eth0]:7882:
bind: cannot assign requested address
```

**Why.** `ice.NewMultiUDPMuxFromPort` binds one socket per interface and fails the whole process if
any single bind fails. A pod with an IPv6 link-local address fails on that address and exits at
start-up. The patch in this repository binds an explicit `udp4` socket instead, and the same call went
from a crash loop to `join p50 11.9 ms`.

**How to check.** `kubectl -n <ns> describe pod <sfu-pod> | grep -E "Restarts|Back-off"`, then read the
previous container's log. A "Running" pod with restarts is a pod that has never been running long.

---

## Numbers, with their provenance

| measurement | value | how |
|---|---|---|
| join p50 / p95, cluster, one candidate | 11.9 / 12.3 ms | kind, 3 participants, `make lab-bench` |
| join p50 / p95, every interface advertised | 149 / 969 ms | loopback, 3 participants, `make measure` |
| SFU CPU per participant | 50.7 ms cpu = 1.0 % of one core | cgroup `cpu.stat`, 3 participants, 15.8 s |
| packets received, 3 participants, 8 s run | 436 | `inbound_packets` from the harness |
| send-side loss (RTCP receiver reports) | not observed | reported as `n/a`, never as 0 |

Loopback numbers describe loopback. The cluster numbers describe one kind node on one laptop. Neither
is a capacity claim, and both are reproducible with the commands above.

## What this lab does not cover

- **No codec or quality work.** No echo testing, no MOS, no TURN. The harness measures reachability,
  timing and packet counts through a real WebRTC stack.
- **One node, one replica.** Multi-node scheduling, and the room-loss that comes with it, is not here.
- **Send-side loss was not measured.** The receiver reports that carry it did not populate in this
  setup; the field is reported as `n/a` rather than as zero. A number that reads as a perfect network
  is worse than no number.
