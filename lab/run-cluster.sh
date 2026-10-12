#!/usr/bin/env bash
# Deploy the chart into the kind cluster, prove which server is answering, run the harness against it,
# and record the result. Every fault script goes through here so that the only thing that changes
# between runs is the knob it names.
#
# usage: lab/run-cluster.sh <label> [helm --set flags...]
set -euo pipefail

label=$1; shift
cd "$(dirname "$0")/.."
mkdir -p lab/evidence

cluster=${CLUSTER:-rtc-bench}
ns=${NS:-rtc}
release=${RELEASE:-sfu}
node=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$cluster-control-plane")
ws_nodeport=30080

# 1. deploy, from the lab's values plus whatever this run changes
helm upgrade --install "$release" charts/sfu -n "$ns" --create-namespace --wait --timeout 120s \
  -f lab/values-kind.yaml "$@" >/dev/null

# 2. prove the pod is the image built from this working tree, and that it is not restarting. An
#    image tag that never changes is how a lab measures a previous build for an hour.
want=$(docker inspect -f '{{.Id}}' rtc-media-bench/sfu:local)
got=$(kubectl -n "$ns" get pod -l app.kubernetes.io/name=sfu \
  -o jsonpath='{.items[0].status.containerStatuses[0].imageID}')
restarts=$(kubectl -n "$ns" get pod -l app.kubernetes.io/name=sfu \
  -o jsonpath='{.items[0].status.containerStatuses[0].restartCount}')
if [[ "$got" != *"${want#sha256:}"* ]]; then
  echo "$label: the running pod is not the image built here"; echo "  want $want"; echo "  got  $got"; exit 1
fi
if [ "$restarts" != "0" ]; then
  echo "$label: the pod has restarted $restarts times: not a clean baseline"
  kubectl -n "$ns" logs deploy/"$release-$release" --previous --tail=5 || true
  exit 1
fi

# 3. the Service has to have an endpoint, or everything below measures a Service, not an SFU
endpoints=$(kubectl -n "$ns" get endpoints "$release-$release" -o jsonpath='{.subsets[*].addresses[*].ip}')
if [ -z "$endpoints" ]; then echo "$label: the Service has no endpoints"; exit 1; fi

# 4. wait for the data path, not for the pod. A freshly replaced pod has an endpoint and a Ready
#    status several seconds before the node's port actually answers, and a lab that measures in that
#    window reports a fault that is really a race - which is exactly what happened the first time
#    these scripts were written.
for attempt in $(seq 1 30); do
  if timeout 3 bash -c "exec 3<>/dev/tcp/$node/$ws_nodeport" 2>/dev/null; then break; fi
  if [ "$attempt" = 30 ]; then echo "$label: signalling never became reachable"; exit 1; fi
  sleep 1
done

# 5. measure
timeout 240 /tmp/bench -url "ws://$node:$ws_nodeport/websocket" -clients "${CLIENTS:-3}" \
  -duration "${DURATION:-8s}" -stagger 400ms -json > "lab/evidence/$label.json" || true

python3 lab/summary.py "lab/evidence/$label.json" "$label"
