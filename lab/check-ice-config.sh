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

python3 lab/check-ice-config.py /tmp/check-ice-config.json "$mux_port" "$advertise"
