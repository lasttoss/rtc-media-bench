#!/usr/bin/env bash
# Renders every branch of the chart that changes the Kubernetes objects, and validates the result.
#
# Both branches matter here: the chart has a configuration that is deliberately broken (media through
# a Service), and a render check that only ever sees the good one would let the broken one rot into
# something that no longer even applies.
set -euo pipefail
cd "$(dirname "$0")/.."

kubeconform=${KUBECONFORM:-}
render() { helm template sfu charts/sfu "$@"; }

# validate <label> <expected kinds, comma separated> [helm flags...]
validate() {
  local label=$1 want=$2; shift 2
  local out kinds
  out=$(render "$@")
  kinds=$(printf '%s' "$out" | sed -n 's/^kind: //p' | sort | paste -sd, -)
  if [ "$kinds" != "$want" ]; then
    echo "$label rendered [$kinds], expected [$want]"
    exit 1
  fi
  printf '%s' "$out" | $kubeconform -strict -summary -ignore-missing-schemas -kubernetes-version 1.31.0 - 2>&1 | sed "s/^/  $label: /"
}

if [ -z "$kubeconform" ]; then
  if command -v kubeconform >/dev/null; then
    kubeconform=kubeconform
  else
    kubeconform="docker run --rm -i ghcr.io/yannh/kubeconform:v0.7.0"
  fi
fi

helm lint --strict charts/sfu >/dev/null
validate "default (hostPort, mux, nodeIP)" "Deployment,NetworkPolicy,Service"
validate "port-range + nodePort + auto" "Deployment,NetworkPolicy,Service" \
  --set ice.mode=range --set exposure=nodePort --set advertise.strategy=auto
validate "hostNetwork + explicit + no policy" "Deployment,Service" \
  --set exposure=hostNetwork --set advertise.strategy=explicit \
  --set advertise.advertiseIP=203.0.113.7 --set networkPolicy.enabled=false
echo "chart ok: every branch rendered and validated"
