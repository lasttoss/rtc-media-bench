#!/usr/bin/env python3
"""Assert the vendored SFU's ICE flags reached the peer connection, by reading what a peer was offered.

Compiling is not evidence. The first version of the ICE patch defined the configuration function and
never called it: it built, it served, and it offered a random port on every interface, so every
measurement taken afterwards described a system nobody had configured.

Only host candidates are asserted on. A peer-reflexive candidate is an address a peer learned from the
packets that happened to arrive; no flag can configure it, and on one host it can even be an address of
this side of the connection. A gate that fails for a working configuration gets deleted rather than
fixed, so the assertion is about the part the flags actually control.
"""
import json
import sys


def split(candidate):
    """'10.0.0.1:7882/host' or '[fe80::1]:7882/host' -> (address, port, type)."""
    addr, _, kind = candidate.rpartition("/")
    ip, _, port = addr.rpartition(":")
    return ip, port, kind


def main():
    path, want_port, want_addr = sys.argv[1], sys.argv[2], sys.argv[3]
    with open(path) as handle:
        summary = json.load(handle)

    detail = summary.get("detail") or []
    if not detail:
        print(json.dumps(summary, indent=2)[:2000])
        sys.exit("the participant produced no detail: the run failed before ICE")

    candidates = detail[0].get("advertised") or []
    hosts = [c for c in candidates if split(c)[2] == "host"]
    if not hosts:
        print(json.dumps(summary, indent=2)[:2000])
        sys.exit(f"no host candidate among {candidates or 'none'}: the advertise flag cannot be verified")

    ports = {split(c)[1] for c in hosts}
    addresses = {split(c)[0] for c in hosts}
    print(f"  {len(candidates)} candidates ({len(hosts)} host), host ports={sorted(ports)}, "
          f"host addresses={sorted(addresses)}")

    if ports != {want_port}:
        sys.exit(f"ICE mux is not in effect: host candidates are on {sorted(ports)}, "
                 f"expected every one on {want_port}")

    if want_addr not in addresses:
        sys.exit(f"advertise-ip is not in effect: host candidates {sorted(addresses)} "
                 f"do not include {want_addr}")

    print(f"  OK: every host candidate is on port {want_port}; {want_addr} is advertised")


if __name__ == "__main__":
    main()
