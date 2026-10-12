#!/usr/bin/env bash
# Fault one: the policy does not allow the pod to answer.
#
# A media pod's egress is not an allow-list of places it calls. ICE sends connectivity checks to
# addresses the pod never addressed first, and the answers to those checks are new flows in the other
# direction. Narrow the egress rule and the pod is Running, Ready, passing every probe, and silent.
#
# The peer has to be inside the cluster for this to bite: pod-to-node traffic is not subject to the
# pod's policy, which is why the first version of this script measured the host and saw nothing.
set -euo pipefail
cd "$(dirname "$0")/../.."
mkdir -p lab/evidence

in_cluster() { # $1 label
  local label=$1
  kubectl -n rtc delete pod bench --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n rtc run bench --image=rtc-media-bench/bench:local --restart=Never \
    --image-pull-policy=IfNotPresent --attach --rm --quiet -- \
    -url ws://sfu-sfu.rtc.svc.cluster.local:8080/websocket -clients 2 -duration 8s -stagger 400ms -json \
    > "lab/evidence/$label.json" 2>/dev/null || true
  python3 lab/summary.py "lab/evidence/$label.json" "$label"
}

echo "== fault 1: egress to peers closed, client inside the cluster =="
helm upgrade --install sfu charts/sfu -n rtc --create-namespace --wait --timeout 120s \
  -f lab/values-kind.yaml --set networkPolicy.egressToPeers=false >/dev/null
in_cluster fault1-egress-closed

echo "== fix: allow the pod to answer =="
helm upgrade --install sfu charts/sfu -n rtc --wait --timeout 120s \
  -f lab/values-kind.yaml --set networkPolicy.egressToPeers=true >/dev/null
in_cluster fault1-egress-open
