#!/usr/bin/env bash
# Fault two: media exposed through a Service.
#
# ICE tells the peer which address and which port to use. A Service translates ports, so the peer is
# sent to a port where nothing is listening - while the websocket keeps answering and the pod keeps
# reporting Ready. This is the shape of a default Service-based deployment, and it is why SFU charts
# expose media with hostPort or hostNetwork instead.
set -euo pipefail
cd "$(dirname "$0")/../.."
echo "== fault 2: media through a Service =="
./lab/run-cluster.sh fault2-service-media --set exposure=nodePort
echo "== fix: hostPort, so the advertised port and the listening port are the same one =="
./lab/run-cluster.sh fault2-fixed --set exposure=hostPort
