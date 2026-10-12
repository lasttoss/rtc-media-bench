#!/usr/bin/env bash
# Fault four: a pod that goes away, with and without a grace period.
#
# The interesting number is not how fast the pod is replaced. It is whether the media kept flowing
# while it was being replaced: SIGTERM at the start of the grace period asks the process to stop
# serving, and a process that ignores it - this one does - keeps the rooms alive until the kubelet
# runs out of patience. With a grace period of zero there is no such window, and every client
# discovers the same thing at the same moment.
set -euo pipefail
cd "$(dirname "$0")/../.."
mkdir -p lab/evidence

node=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' rtc-bench-control-plane)
ws=30080

wait_for_path() {
  for _ in $(seq 1 30); do
    timeout 3 bash -c "exec 3<>/dev/tcp/$node/$ws" 2>/dev/null && return 0
    sleep 1
  done
  echo "signalling never became reachable"; exit 1
}

run_case() { # $1 label, $2 grace seconds
  local label=$1 grace=$2
  helm upgrade --install sfu charts/sfu -n rtc --create-namespace --wait --timeout 120s \
    -f lab/values-kind.yaml --set terminationGracePeriodSeconds="$grace" >/dev/null
  wait_for_path
  /tmp/bench -url "ws://$node:$ws/websocket" -clients 3 -duration 25s -stagger 400ms \
    -json > "lab/evidence/$label.json" &
  local bench=$!
  sleep 9
  echo "  deleting the pod (grace ${grace}s) while 3 participants are in a call"
  kubectl -n rtc delete pod -l app.kubernetes.io/name=sfu --grace-period="$grace" --wait=false >/dev/null
  wait $bench || true
  python3 lab/summary.py "lab/evidence/$label.json" "$label"
  local inbound
  inbound=$(python3 -c "import json;print(json.load(open('lab/evidence/$label.json'))['inbound_packets'])")
  echo "      packets received after the delete was issued: $inbound"
}

run_case fault4-no-grace 0
run_case fault4-grace-30 30
./lab/run-cluster.sh fault4-fixed
