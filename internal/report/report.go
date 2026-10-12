// Package report turns a set of participant results into the table this repository quotes.
//
// Percentiles are nearest-rank, the same rule the other repositories in this portfolio use, so
// a p95 here is a number some participant actually experienced rather than an interpolation
// between two of them.
package report

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Summary is everything the harness reports, with no dependency on a live peer connection.
type Summary struct {
	Participants int       `json:"participants"`
	Join         []float64 `json:"join_ms"`        // ICE connected
	FirstFrame   []float64 `json:"first_frame_ms"` // those that received media
	RTT          []float64 `json:"rtt_ms"`         // nominated ICE pair
	UploadRTT    []float64 `json:"upload_rtt_ms"`  // from the SFU's receiver reports
	Jitter       []float64 `json:"jitter_ms"`
	DownKbps     []float64 `json:"down_kbps"`
	UpKbps       []float64 `json:"up_kbps"`
	FramesSent   int       `json:"frames_sent"`
	InboundRecv  uint32    `json:"inbound_packets"`
	InboundLost  uint32    `json:"inbound_lost"`
	UploadSent   uint32    `json:"upload_packets"`
	UploadLost   uint32    `json:"upload_lost"`
	Disconnects  int       `json:"disconnects"`
	// Survived is how long each participant's call lasted, in ms. With a fault that takes the server
	// away, this is the number that says how long the damage took to arrive.
	Survived []float64 `json:"survived_ms"`
	Failed   []string  `json:"failed,omitempty"`
	Detail   []Detail  `json:"detail,omitempty"`
}

// Detail is one participant's own timings, kept apart so a report can show which phase took the
// time. A join of 1200 ms that is 20 ms of dialling and 1180 ms of ICE gathering is a different
// problem from one that spent 1200 ms waiting for an answer.
type Detail struct {
	Index        int     `json:"index"`
	DialMS       float64 `json:"dial_ms"`
	OfferMS      float64 `json:"offer_ms"`
	IceMS        float64 `json:"ice_ms"`
	FirstFrameMS float64 `json:"first_frame_ms"`
	// Advertised, LocalIP/RemoteIP and RemotePort come from the nominated pair's stats: they are
	// the addresses media actually flows between, and the peer's whole candidate list.
	Advertised []string `json:"advertised,omitempty"`
	LocalIP    string   `json:"local_ip,omitempty"`
	LocalPort  int      `json:"local_port,omitempty"`
	RemoteIP   string   `json:"remote_ip,omitempty"`
	RemotePort int      `json:"remote_port,omitempty"`
	RemoteType string   `json:"remote_candidate_type,omitempty"`
}

// Percentile returns the nearest-rank percentile of values. Empty input is 0 rather than NaN:
// a report with no data should read as "nothing to report", not as a number that poisons every
// average it touches.
func Percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[rank-1]
}

// WithDetail attaches the per-participant timings, in the order the participants ran.
func (s Summary) WithDetail(d []Detail) Summary {
	s.Detail = d
	return s
}

func ms(v float64) string {
	if v <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f", v)
}

// Text renders the table the README and the runbook quote.
func (s Summary) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "participants      %d\n", s.Participants)
	if len(s.Join) == 0 {
		fmt.Fprintf(&b, "join              nobody established a connection\n")
	}
	fmt.Fprintf(&b, "join              p50 %s ms  p95 %s ms  (dial to ICE connected)\n",
		ms(Percentile(s.Join, 50)), ms(Percentile(s.Join, 95)))
	if len(s.FirstFrame) > 0 {
		fmt.Fprintf(&b, "first frame       p50 %s ms  p95 %s ms  (dial to the first RTP packet from the SFU)\n",
			ms(Percentile(s.FirstFrame, 50)), ms(Percentile(s.FirstFrame, 95)))
	} else {
		fmt.Fprintf(&b, "first frame       n/a (nobody else was publishing)\n")
	}
	fmt.Fprintf(&b, "rtt               p50 %s ms  p95 %s ms  (nominated ICE pair)\n",
		ms(Percentile(s.RTT, 50)), ms(Percentile(s.RTT, 95)))
	fmt.Fprintf(&b, "upload rtt        p50 %s ms  p95 %s ms  (from the SFU's receiver reports)\n",
		ms(Percentile(s.UploadRTT, 50)), ms(Percentile(s.UploadRTT, 95)))
	fmt.Fprintf(&b, "jitter            p50 %s ms  p95 %s ms\n",
		ms(Percentile(s.Jitter, 50)), ms(Percentile(s.Jitter, 95)))
	fmt.Fprintf(&b, "bitrate           down p50 %s kbps  up p50 %s kbps\n",
		ms(Percentile(s.DownKbps, 50)), ms(Percentile(s.UpKbps, 50)))
	fmt.Fprintf(&b, "packets           inbound %d received / %d lost\n", s.InboundRecv, s.InboundLost)
	// Sender-side loss can only come from the receiver's RTCP reports, and when they do not arrive
	// the honest answer is that the number is missing - not zero. Zero would read as a perfect
	// network, which is the one thing this line must never accidentally claim.
	if s.UploadSent > 0 {
		fmt.Fprintf(&b, "                  upload %d sent / %d lost\n", s.UploadSent, s.UploadLost)
	} else if s.FramesSent > 0 {
		fmt.Fprintf(&b, "                  upload n/a (no receiver reports observed; %d frames sent)\n", s.FramesSent)
	}
	fmt.Fprintf(&b, "disconnects       %d\n", s.Disconnects)
	if len(s.Survived) > 0 {
		fmt.Fprintf(&b, "survived          p50 %s ms  p95 %s ms  (how long the call lasted)\n",
			ms(Percentile(s.Survived, 50)), ms(Percentile(s.Survived, 95)))
	}
	fmt.Fprintf(&b, "failed            %d\n", len(s.Failed))
	for _, f := range s.Failed {
		fmt.Fprintf(&b, "  - %s\n", f)
	}
	return b.String()
}
