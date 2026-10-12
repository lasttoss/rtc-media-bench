#!/usr/bin/env bash
# Fault three: the SFU advertises the address it thinks it has.
#
# Inside a pod that address is a pod CIDR address, reachable by nothing outside the cluster. This is
# the failure that looks like a client problem: signalling works, the pod is Ready, every probe
# passes, and the call never connects.
set -euo pipefail
cd "$(dirname "$0")/../.."
echo "== fault 3: advertise the pod's own address =="
./lab/run-cluster.sh fault3-advertise-podip --set advertise.strategy=auto
./lab/run-cluster.sh fault3-fixed --set advertise.strategy=nodeIP
