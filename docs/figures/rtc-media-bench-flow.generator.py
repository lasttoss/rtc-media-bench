#!/usr/bin/env python3
"""Generate docs/figures/rtc-media-bench-flow.html.

Measured, not positioned by eye: every box is sized from its own text, so editing a line below
cannot overlap anything. Same shape as the generators in this portfolio, for the same reason.

    python3 docs/figures/rtc-media-bench-flow.generator.py
    python3 <diagram-design>/scripts/self_check.py docs/figures/rtc-media-bench-flow.html
    make diagram
"""
import html
import pathlib

PAPER, INK, MUTED, ACCENT, LINE, TINT, BAD = "#f5f5f5", "#2d3142", "#4f5d75", "#b3541e", "#c9ccd1", "#fdf4ef", "#8c2f1f"
FONT = {"sans": "'Geist', system-ui, sans-serif",
        "mono": "'Geist Mono', ui-monospace, monospace",
        "serif": "'Instrument Serif', serif"}
ADVANCE = {"mono": 0.605, "sans": 0.525, "sans-bold": 0.565}
PAD, GAP = 16, 110


def esc(s):
    return html.escape(str(s))


def width(s, size, family="sans", weight="400"):
    key = "mono" if family == "mono" else ("sans-bold" if weight == "600" else "sans")
    return len(s) * ADVANCE[key] * size


def text(x, y, s, size=12.5, weight="400", fill=INK, family="sans", anchor="start"):
    return (f'<text x="{x:.0f}" y="{y:.0f}" font-family="{FONT[family]}" font-size="{size}" '
            f'font-weight="{weight}" fill="{fill}" text-anchor="{anchor}">{esc(s)}</text>')


def rect(x, y, w, h, fill="#ffffff", stroke=LINE, rx=10):
    return f'<rect x="{x:.0f}" y="{y:.0f}" width="{w:.0f}" height="{h:.0f}" rx="{rx}" fill="{fill}" stroke="{stroke}"/>'


def arrow(x1, y1, x2, y2, label="", dashed=False):
    dash = ' stroke-dasharray="5 4"' if dashed else ""
    out = [f'<line x1="{x1:.0f}" y1="{y1:.0f}" x2="{x2:.0f}" y2="{y2:.0f}" stroke="{MUTED}" stroke-width="1.6" '
           f'marker-end="url(#arrowhead)"{dash}/>']
    if label:
        mx, my = (x1 + x2) / 2, (y1 + y2) / 2
        lw = width(label, 11, "mono") + 12
        out.append(rect(mx - lw / 2, my - 12, lw, 20, fill=PAPER, stroke="none", rx=5))
        out.append(text(mx, my + 2, label, size=11, fill=MUTED, family="mono", anchor="middle"))
    return "".join(out)


class Block:
    def __init__(self, title, subtitle, lines, tint=None):
        self.title, self.subtitle, self.lines, self.tint = title, subtitle, lines, tint
        widths = [width(title, 15, weight="600")]
        if subtitle:
            widths.append(width(subtitle, 11.5, "mono"))
        for line, mono in lines:
            widths.append(width(line, 11.5, "mono" if mono else "sans"))
        self.w = max(widths) + PAD * 2
        self.h = PAD * 2 + 22 + (16 if subtitle else 0) + len(lines) * 17

    def render(self, x, y):
        out = [rect(x, y, self.w, self.h, fill=self.tint or "#ffffff")]
        out.append(text(x + PAD, y + PAD + 12, self.title, size=15, weight="600"))
        yy = y + PAD + 34
        if self.subtitle:
            out.append(text(x + PAD, yy + 4, self.subtitle, size=11.5, fill=MUTED, family="mono"))
            yy += 16
        for line, mono in self.lines:
            out.append(text(x + PAD, yy + 9, line, size=11.5, family="mono" if mono else "sans"))
            yy += 17
        return "".join(out)


CLIENT = Block("participants", "headless, real VP8, in or out of the cluster", [
    ("ws://host:30080/websocket   signalling", True),
    ("udp to the address the SFU advertises", True),
    ("harness/ - Go + pion, no key, no cloud", True),
])
SFU = Block("SFU in a pod", "pion sfu-ws, vendored + one ICE patch", [
    ("one UDP port, 7882   (-udp-mux-port)", True),
    ("advertise status.hostIP  (-advertise-ip)", True),
    ("hostPort 7882    <- the port IS the port", True),
    ("NetworkPolicy on by default", True),
])
GOOD = Block("this is what makes a call", "measured, kind cluster", [
    ("advertised   172.19.0.2:7882/host   1 host candidate", True),
    ("join p50 11.9 ms   p95 12.3 ms", True),
    ("SFU CPU 50.7 ms/participant  = 1.0% core", True),
])
BAD = Block("this is what a Service does", "reproduced: fault 2", [
    ("advertised   172.19.0.2:7882", True),
    ("listening    nodePort 30782   <- not the same port", True),
    ("join n=0   inbound 0   and NO error reported", True),
], tint="#fdf4ef")
MEASURE = Block("what the harness reports, and why it is split", "one number would hide the fault", [
    ("dial  17 ms   the websocket answered", True),
    ("offer  2 ms   the SFU sent an SDP", True),
    ("ice  12 ms   a path was found  <- faults live here", True),
    ("join   = the last of those, not the first", True),
])
FAULTS = Block("the fault catalogue, as measured", "docs/runbook.md", [
    ("1  egress policy blocks the answer        not reproduced", True),
    ("2  media port translated by a Service     reproduced", True),
    ("3  advertise the pod's own address        reproduced", True),
    ("4  pod deleted, grace period 0 or 30      reproduced", True),
    ("5  advertise every interface              20 candidates, p95 434 ms", True),
])
CLAIMS = [
    "a healthy websocket and a Ready pod say nothing about media",
    "a fault can fail with no error: report what happened, not what raised",
    "missing data prints as n/a; a zero reads as a perfect network",
]

col1 = max(CLIENT.w, MEASURE.w)
col2 = SFU.w
col3 = max(GOOD.w, BAD.w, FAULTS.w)
MARGIN, TOP = 28, 74
x1, x2 = MARGIN, MARGIN + col1 + GAP
x3 = x2 + col2 + GAP
total_w = x3 + col3 + MARGIN

y = TOP
client_y, measure_y = y + 20, y + 20 + CLIENT.h + 40
sfu_y, good_y = y, y
bad_y = good_y + GOOD.h + 40
eval_y = measure_y
faults_y = bad_y + BAD.h + 40
strip_y = max(faults_y + FAULTS.h, measure_y + MEASURE.h) + 42
strip_h = 78
H = strip_y + strip_h + 46

svg = [f'<svg viewBox="0 0 {total_w:.0f} {H:.0f}" role="img" aria-labelledby="fig-title fig-desc" xmlns="http://www.w3.org/2000/svg">']
svg.append('<title id="fig-title">rtc-media-bench: where a WebRTC call actually breaks, and what is measured</title>')
svg.append('<desc id="fig-desc">Participants signal over a websocket through a Service and send media to the address '
           'the SFU advertises. With hostPort exposure and a single multiplexed UDP port the SFU offers one candidate, '
           'the node address on port 7882, and three participants join with a median of 11.9 milliseconds using about '
           'fifty milliseconds of CPU each. When media is exposed through a Service the advertised port and the '
           'listening port differ, no participant joins, and no error is reported anywhere. The harness reports dial, '
           'offer and ICE separately because every fault in the catalogue appears in the ICE number. Beneath, the '
           'fault catalogue as measured: an egress policy that did not reproduce, a translated media port that did, a '
           'pod advertising its own address that did, a pod deletion with and without a grace period that behaved '
           'identically, and a peer advertising every interface it owns costing 969 milliseconds at p95 against 18.</desc>')
svg.append(f'<defs><marker id="arrowhead" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" '
           f'orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="{MUTED}"/></marker></defs>')

svg.append(CLIENT.render(x1, client_y))
svg.append(MEASURE.render(x1, measure_y))
svg.append(SFU.render(x2, sfu_y))
svg.append(GOOD.render(x3, good_y))
svg.append(BAD.render(x3, bad_y))
svg.append(FAULTS.render(x3, faults_y))

svg.append(arrow(x1 + col1, client_y + 46, x2, sfu_y + 40, "signalling"))
svg.append(arrow(x2 + col2, sfu_y + 40, x3, good_y + 30, "media"))
svg.append(text(MARGIN, H - 16, "make lab-up  ·  make measure  ·  lab/faults/*.sh  — every number here comes from one of these",
                size=11, fill=MUTED, family="mono"))
svg.append(rect(MARGIN, strip_y, total_w - MARGIN * 2, strip_h))
svg.append(text(MARGIN + PAD, strip_y + 26, "three rules this repository keeps, each one because breaking it produced a wrong number",
                size=12.5, weight="600", fill=MUTED))
chip_w = (total_w - MARGIN * 2 - PAD * 2 - 24) / 3
# A generator that clips a claim is a generator that ships a figure nobody can read, and the clipping
# is invisible in the HTML: it only shows in the export, which is the artefact people look at. This
# fails instead, naming the claim that does not fit.
for i, claim in enumerate(CLAIMS):
    need = width(f"{i + 1}. {claim}", 11.5) + 20
    if need > chip_w:
        raise SystemExit(f"claim {i + 1} needs {need:.0f}px, the strip gives {chip_w:.0f}px: shorten it")
for i, claim in enumerate(CLAIMS):
    cx = MARGIN + PAD + i * (chip_w + 12)
    svg.append(rect(cx, strip_y + 38, chip_w, 28, fill=TINT, stroke="#e8cdbd", rx=6))
    svg.append(text(cx + 10, strip_y + 57, f"{i + 1}. {claim}", size=11.5))
svg.append("</svg>")

meta = ('<!-- diagram-meta {"topic":"rtc-media-bench-flow","level":"runtime","type":"flowchart",'
        '"question":"Where does a WebRTC call actually break, and what does the harness measure?",'
        '"sources":["charts/sfu/values.yaml","charts/sfu/templates/deployment.yaml","charts/sfu/templates/service.yaml",'
        '"charts/sfu/templates/networkpolicy.yaml","internal/rtc/client.go","internal/rtc/stats.go","cmd/bench/main.go",'
        '"lab/run-cluster.sh","lab/measure.sh","lab/measure-sfu-cpu.sh","lab/faults/02-media-through-a-service.sh",'
        '"lab/faults/03-advertise-pod-ip.sh","lab/faults/04-restart-without-grace.sh","docs/runbook.md","Dockerfile","docker-compose.yml","Makefile"],'
        '"updated":"2026-10-12"} -->')

page = f"""{meta}
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>rtc-media-bench</title>
<link href="https://fonts.googleapis.com/css2?family=Instrument+Serif:ital@0;1&family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<style>
  *, *::before, *::after {{ box-sizing: border-box; margin: 0; padding: 0; }}
  :root {{
    --color-paper: {PAPER}; --color-ink: {INK}; --color-muted: {MUTED}; --color-accent: {ACCENT};
    --font-sans: 'Geist', system-ui, sans-serif; --font-serif: 'Instrument Serif', serif;
    --font-mono: 'Geist Mono', ui-monospace, monospace;
  }}
  body {{ font-family: var(--font-sans); background: var(--color-paper); color: var(--color-ink);
         min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 3rem 2rem; }}
  .frame {{ max-width: {total_w + 40:.0f}px; width: 100%; }}
  .eyebrow {{ font-family: var(--font-mono); font-size: 0.66rem; font-weight: 500; letter-spacing: 0.18em;
              text-transform: uppercase; color: var(--color-muted); margin-bottom: 0.5rem; }}
  h1 {{ font-family: var(--font-serif); font-size: clamp(1.5rem, 2.4vw + 0.75rem, 2rem); font-weight: 400;
        letter-spacing: -0.02em; line-height: 1.15; color: var(--color-ink); margin-bottom: 1.5rem; }}
  .diagram-container {{ width: 100%; overflow-x: auto; }}
  svg {{ width: 100%; max-width: {total_w:.0f}px; display: block; }}
  .caption {{ font-family: var(--font-mono); font-size: 0.7rem; color: var(--color-muted); margin-top: 1rem; }}
</style>
</head>
<body>
<div class="frame">
  <p class="eyebrow">rtc-media-bench · runtime + fault lab</p>
  <h1>Where a call breaks, and the four faults this lab reproduces on purpose</h1>
  <div class="diagram-container">
{"".join(svg)}
  </div>
  <p class="caption">Every fault in the catalogue lives in the ICE number, not in the websocket and not in the pod
  status: signalling and media fail separately, and the tools that report "healthy" are the ones that make this class
  of fault expensive. One fault did not reproduce here at all, and the runbook says so with the four configurations
  that were tried.</p>
</div>
</body>
</html>
"""
out = pathlib.Path(__file__).with_name("rtc-media-bench-flow.html")
out.write_text(page, encoding="utf-8")
print(f"wrote {out.name}: {total_w:.0f}x{H:.0f} viewBox, {len(page)} bytes")
