#!/usr/bin/env bash
# Bring the lab up from nothing: a kind cluster, a CNI that enforces policy, both images, the chart.
set -euo pipefail
cd "$(dirname "$0")/.."
cluster=${CLUSTER:-rtc-bench}

kind get clusters 2>/dev/null | grep -qx "$cluster" || kind create cluster --name "$cluster" --wait 180s

# kind's default CNI accepts NetworkPolicy objects and ignores them, so a fault that depends on one
# can never reproduce. Calico is installed so that the answer to "is the policy doing anything?" is
# a measurement rather than an assumption.
if ! kubectl -n kube-system get daemonset calico-node >/dev/null 2>&1; then
  kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.2/manifests/calico.yaml >/dev/null
  kubectl -n kube-system rollout status daemonset/calico-node --timeout=180s
fi

docker build -q -t rtc-media-bench/sfu:local -f lab/sfu/Dockerfile .
docker build -q -t rtc-media-bench/bench:local -f lab/bench/Dockerfile .
kind load docker-image rtc-media-bench/sfu:local --name "$cluster"
kind load docker-image rtc-media-bench/bench:local --name "$cluster"

helm upgrade --install sfu charts/sfu -n rtc --create-namespace --wait --timeout 120s -f lab/values-kind.yaml
kubectl -n rtc get pods -o custom-columns='NAME:.metadata.name,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount'
