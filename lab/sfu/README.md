# The SFU this lab runs

Upstream [pion/example-webrtc-applications `sfu-ws`](https://github.com/pion/example-webrtc-applications),
MIT, **upstream commit 6b76bbb5518f** (2026-10-10). It is a websocket-signalled SFU: a JSON message
carrying an SDP offer or answer, and RTP forwarded to every other participant in the room.

## What was changed, and why

One patch, `patches/0001-ice-config.patch`, adds `lab/sfu/ice.go` and five flags to `main.go`:
`-udp-mux-port`, `-udp-port-min`, `-udp-port-max`, `-advertise-ip`, `-ice-tcp-port`. Nothing in the
media path is touched.

The reason is that upstream leaves ICE to pion's defaults, and the defaults are the wrong answer for
a cluster: a random UDP port per candidate, on every interface the host has, with no way to say which
address a peer should be told about. Those defaults are not a bug on a laptop - they are exactly what
this lab needs to *reproduce*, because they are what a Service, a firewall and a NAT each disagree
with.

`make verify-patch` re-downloads upstream at the pinned commit, applies the committed patch, and
fails unless the result matches `lab/sfu/` byte for byte. Compiling is not enough: the first version
of this patch defined the ICE function and never called it, which built, served, and measured a
system nobody had configured.
