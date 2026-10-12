#!/usr/bin/env bash
# Run one measurement against one SFU configuration, and refuse to report on an SFU it did not start.
#
# The first version of this lab reused a single port and killed by process name. It reported a
# 17 ms join for a configuration whose advertised address cannot be reached, because the client was
# talking to an older SFU that was still listening. A measurement that cannot prove which server it
# measured is not a measurement.
#
# usage: lab/measure.sh <label> <clients> <duration> [extra sfu flags...]
set -euo pipefail

label=$1; clients=$2; duration=$3; shift 3
cd "$(dirname "$0")/.."
mkdir -p lab/evidence

sfu=${SFU_BIN:-/tmp/sfu}
bench=${BENCH_BIN:-/tmp/bench}
ws_port=${WS_PORT:-17880}

[ -x "$sfu" ] || { echo "no sfu binary at $sfu"; exit 1; }
[ -x "$bench" ] || { echo "no bench binary at $bench"; exit 1; }

# The port has to be ours before we start, and nothing else may answer on it after.
if ss -ltn 2>/dev/null | awk 'NR>1 {print $4}' | sed 's/.*://' | grep -qx "$ws_port"; then
  echo "port $ws_port is already taken: refusing to measure against whatever is on it"
  ss -ltnp 2>/dev/null | grep ":$ws_port" || true
  exit 1
fi

log=lab/evidence/$label.sfu.log
json=lab/evidence/$label.json
cleanup() { [ -f "$log.pid" ] && kill "$(cat "$log.pid")" 2>/dev/null || true; rm -f "$log.pid"; }
trap cleanup EXIT

( cd lab/sfu && exec "$sfu" -addr ":$ws_port" "$@" > "../evidence/$label.sfu.log" 2>&1 ) &
echo $! > "$log.pid"
sleep 1

if ! ss -ltn 2>/dev/null | awk 'NR>1 {print $4}' | sed 's/.*://' | grep -qx "$ws_port"; then
  echo "the SFU did not come up: not measuring anything"
  cat "$log"
  exit 1
fi
if grep -q "Failed to start http server" "$log"; then
  echo "the SFU could not bind: not measuring anything"
  cat "$log"
  exit 1
fi

"$bench" -url "ws://127.0.0.1:$ws_port/websocket" -clients "$clients" -duration "$duration" \
  -stagger 400ms -json > "$json" || true

cleanup
trap - EXIT

python3 lab/summary.py "lab/evidence/$label.json" "$label"
