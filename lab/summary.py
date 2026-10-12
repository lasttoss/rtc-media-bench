#!/usr/bin/env python3
"""One line per measurement, for the lab scripts and the README table.

Kept as a file rather than an inline heredoc because the inline version needed three levels of
quoting (bash -c inside a heredoc inside a shell function) and broke twice on exactly that.
"""
import json
import sys


def rank(values, p):
    if not values:
        return None
    values = sorted(values)
    return round(values[max(0, int(p / 100 * len(values)) - 1)], 1)


def main():
    path, label = sys.argv[1], sys.argv[2]
    with open(path) as handle:
        d = json.load(handle)
    join = d.get("join_ms") or []
    detail = d.get("detail") or [{}]
    advertised = detail[0].get("advertised")
    parts = [
        f"{label:<26}",
        f"join p50={rank(join, 50)} p95={rank(join, 95)} ms (n={len(join)})",
        f"adv={advertised}",
        f"inbound={d.get('inbound_packets')}",
        f"failed={len(d.get('failed') or [])}",
    ]
    print("  " + " | ".join(str(p) for p in parts))
    for f in (d.get("failed") or [])[:1]:
        print("      " + f[:120])


if __name__ == "__main__":
    main()
