#!/usr/bin/env bash
# CPU spent by the SFU pod per participant, read from the cgroup the container actually lives in.
#
# Kubernetes does not expose CPU per pod without a metrics server, and adding one to a lab would be a
# second system to debug. The node's cgroup filesystem has the number already: find the pod's
# cpu.stat, read usage_usec before and after a call, and divide.
set -euo pipefail
cd "$(dirname "$0")/.."
cluster=${CLUSTER:-rtc-bench}
clients=${1:-3}
duration=${2:-20s}

stat_path() {
  # The container id is the reliable handle: systemd names the scope after it
  # (cri-containerd-<id>.scope), and looking for "the pod cgroup" by uid found kubelet's own slice
  # first, which reported exactly zero CPU for a call that plainly used some.
  cid=$(kubectl -n rtc get pod -l app.kubernetes.io/name=sfu \
    -o jsonpath='{.items[0].status.containerStatuses[0].containerID}' | sed 's|containerd://||')
  docker exec "$cluster-control-plane" sh -c \
    "find /sys/fs/cgroup -name cpu.stat -path '*$cid*' 2>/dev/null | head -1"
}

usage() { docker exec "$cluster-control-plane" sh -c "awk '/usage_usec/ {print \$2}' '$1'"; }

node=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$cluster-control-plane")
path=$(stat_path)
if [ -z "$path" ]; then echo "could not find the pod cgroup; is the release installed?"; exit 1; fi
echo "  cgroup: $path"

before=$(usage "$path")
start=$(date +%s%3N)
/tmp/bench -url "ws://$node:30080/websocket" -clients "$clients" -duration "$duration" -stagger 400ms \
  -json > /tmp/cpu-run.json || true
end=$(date +%s%3N)
sleep 1
after=$(usage "$path")

python3 - "$before" "$after" "$clients" "$((end - start))" <<'PY'
import sys
before, after, clients, wall = (int(x) for x in sys.argv[1:5])
cpu_ms = (after - before) / 1000
print(f"  SFU CPU: {cpu_ms:.0f} ms over {wall} ms of wall clock, {clients} participants")
print(f"  per participant: {cpu_ms / clients:.1f} ms cpu = {100 * cpu_ms / wall:.1f}% of one core (whole run)")
PY
python3 lab/summary.py /tmp/cpu-run.json "cpu-run(${clients}p)"
