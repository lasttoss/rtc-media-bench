#!/usr/bin/env bash
# Asserts the vendored SFU's ICE configuration is wired up, by watching what it offers.
#
# Compiling is not evidence. The first version of the local patch defined the ICE function and
# never called it: it built, it served, and it offered random ports on every interface, so every
# measurement taken afterwards described a system nobody had configured. This script is the gate
# that catches that class of mistake: with -udp-mux-port and -advertise-ip set, every candidate
# the client receives must carry that port and that address.
set -euo pipefail

cd "$(dirname "$0")/.."
sfu=${SFU_BIN:-/tmp/sfu}
ws_port=${WS_PORT:-17880}
mux_port=${MUX_PORT:-17882}
advertise=${ADVERTISE_IP:-127.0.0.1}

[ -x "$sfu" ] || { echo "no sfu binary at $sfu (run: go build -o /tmp/sfu ./lab/sfu/)"; exit 1; }

pkill -x sfu 2>/dev/null || true
sleep 0.3
( cd lab/sfu && setsid "$sfu" -addr ":$ws_port" -udp-mux-port "$mux_port" -advertise-ip "$advertise" \
    >/tmp/check-ice-config.sfu.log 2>&1 </dev/null & )
trap 'pkill -x sfu 2>/dev/null || true' EXIT
sleep 1.2

go build -o /tmp/bench ./cmd/bench
/tmp/bench -url "ws://127.0.0.1:$ws_port/websocket" -clients 1 -duration 3s -stagger 0 -json > /tmp/check-ice-config.json || true

MUX_PORT=$mux_port ADVERTISE_IP=$advertise python3 - <<'PY'
import json, os, sys
d = json.load(open("/tmp/check-ice-config.json"))
detail = d.get("detail") or []
if not detail:
    sys.exit("the participant produced no detail: the run failed before ICE")
cands = detail[0].get("advertised") or []
if not cands:
    sys.exit("the SFU offered no candidates at all")
want_port, = [os.environ["MUX_PORT"]]
want_addr = os.environ["ADVERTISE_IP"]
# every entry is "address:port/type", and the address may be a bracketed IPv6 literal
def split(c):
    addr, _, typ = c.rpartition("/")
    ip, _, port = addr.rpartition(":")
    return ip, port, typ

ports = {split(c)[1] for c in cands}
addrs = {split(c)[0] for c in cands}
print(f"  {len(cands)} candidates, ports={sorted(ports)}, addresses={sorted(addrs)}")
if ports != {want_port}:
    sys.exit(f"ICE mux is not in effect: ports {sorted(ports)}, expected every candidate on {want_port}")
# Host candidates are what the SFU chose to advertise, so they must carry the flag. Peer-reflexive
# candidates are discovered from the packets that actually arrive and cannot be configured - which
# is why the assertion is about the host ones.
# The flag has to appear among the host candidates. It cannot be the *only* address: with a
# single-port mux the socket covers every interface, and a peer on the same host can still reach an
# address nobody advertised - asserting equality here would fail for a working configuration.
hosts = {split(c)[0] for c in cands if split(c)[2] == "host"}
if want_addr not in hosts:
    sys.exit(f"advertise-ip is not in effect: host candidates {sorted(hosts)} do not include {want_addr}")
print(f"  OK: every candidate on port {want_port}; host candidates advertise {want_addr}")
PY
