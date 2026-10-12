package rtc

import (
	"net"
	"sort"
	"strconv"

	"github.com/pion/webrtc/v4"
)

// Sample is what one peer connection reports about itself at a moment in time. It is a plain
// struct so the extraction can be tested against a fixture report instead of a live call.
type Sample struct {
	// IceRTTMS is the round trip time of the nominated candidate pair, as measured by STUN.
	IceRTTMS float64
	// UploadRTTMS is the round trip time the remote reported in its RTCP receiver reports, i.e.
	// the RTT of the leg that actually carries the media we send.
	UploadRTTMS float64
	// JitterMS is the inbound jitter estimate.
	JitterMS float64
	// InboundReceived/InboundLost are what arrived from the SFU.
	InboundReceived, InboundLost uint32
	// UploadSent/UploadLost are what the SFU says arrived from us: the only place packet loss on
	// the way out is visible, because the sender cannot see it.
	UploadSent, UploadLost uint32
	FractionLost           float64
	BytesReceived          uint64
	BytesSent              uint64

	// LocalIP/LocalPort and RemoteIP/RemotePort describe the nominated pair: the addresses media
	// is actually flowing between.
	LocalIP, RemoteIP     string
	LocalPort, RemotePort int
	// RemoteCandidateType is host, srflx, prflx or relay. It is the difference between "the peer
	// is on the same network" and "the peer is behind something", which is often the whole answer.
	RemoteCandidateType string
	// Advertised are all the addresses the peer offered in ICE candidates. This is the field that
	// explains a slow join and the one the lab asserts on: a peer that advertises every interface
	// it owns looks exactly like a peer that is broken, except it eventually works.
	Advertised []string
}

// lostOf converts the receiver-reported cumulative loss, which the spec makes signed (RFC 3550
// allows a negative count when duplicates arrive). A negative "lost" is not a loss, so it is
// clamped rather than wrapped into a huge unsigned number.
func lostOf(n int32) uint32 {
	if n < 0 {
		return 0
	}
	return uint32(n)
}

// Extract folds a stats report into a Sample. Streams are summed, which is what you want when
// the question is "did this participant's call survive", not "which SSRC jittered".
func Extract(rep webrtc.StatsReport) Sample {
	var out Sample

	// Pass one: the candidate pairs. The nominated one carries the media, and its two ids point
	// at the local and remote candidates, which is the only way stats names an address.
	remoteIDs := map[string]struct{}{}
	var pair webrtc.ICECandidatePairStats
	var havePair bool
	for _, v := range rep {
		var p *webrtc.ICECandidatePairStats
		switch s := v.(type) {
		case webrtc.ICECandidatePairStats:
			p = &s
		case *webrtc.ICECandidatePairStats:
			p = s
		}
		if p == nil {
			continue
		}
		remoteIDs[p.RemoteCandidateID] = struct{}{}
		if p.State == webrtc.StatsICECandidatePairStateSucceeded && p.Nominated {
			pair, havePair = *p, true
		}
	}
	if havePair {
		out.IceRTTMS = pair.CurrentRoundTripTime * 1000
		out.BytesSent = pair.BytesSent
		if c, ok := candidate(rep, pair.RemoteCandidateID); ok {
			out.RemoteIP, out.RemotePort = c.IP, int(c.Port)
			out.RemoteCandidateType = candidateTypeName(c.CandidateType)
		}
		if c, ok := candidate(rep, pair.LocalCandidateID); ok {
			out.LocalIP, out.LocalPort = c.IP, int(c.Port)
		}
	}

	// Pass two: the streams, and every candidate the peer offered.
	for _, v := range rep {
		switch s := v.(type) {
		case webrtc.InboundRTPStreamStats:
			out.InboundReceived += s.PacketsReceived
			out.InboundLost += lostOf(s.PacketsLost)
			out.BytesReceived += s.BytesReceived
			if s.Jitter > out.JitterMS/1000 {
				out.JitterMS = s.Jitter * 1000
			}
		case *webrtc.InboundRTPStreamStats:
			out.InboundReceived += s.PacketsReceived
			out.InboundLost += lostOf(s.PacketsLost)
			out.BytesReceived += s.BytesReceived
			if s.Jitter > out.JitterMS/1000 {
				out.JitterMS = s.Jitter * 1000
			}
		case webrtc.RemoteInboundRTPStreamStats:
			lost := lostOf(s.PacketsLost)
			out.UploadSent += s.PacketsReceived + lost
			out.UploadLost += lost
			if s.RoundTripTime > 0 {
				out.UploadRTTMS = s.RoundTripTime * 1000
			}
			if s.FractionLost > out.FractionLost {
				out.FractionLost = s.FractionLost
			}
		case *webrtc.RemoteInboundRTPStreamStats:
			lost := lostOf(s.PacketsLost)
			out.UploadSent += s.PacketsReceived + lost
			out.UploadLost += lost
			if s.RoundTripTime > 0 {
				out.UploadRTTMS = s.RoundTripTime * 1000
			}
			if s.FractionLost > out.FractionLost {
				out.FractionLost = s.FractionLost
			}
		case webrtc.ICECandidateStats:
			if _, remote := remoteIDs[s.ID]; remote {
				out.Advertised = append(out.Advertised, addrOf(s))
			}
		case *webrtc.ICECandidateStats:
			if _, remote := remoteIDs[s.ID]; remote {
				out.Advertised = append(out.Advertised, addrOf(*s))
			}
		}
	}
	sort.Strings(out.Advertised)
	return out
}

// candidateTypeName spells the ICECandidateType out. A conversion from the int type compiles and
// yields a single rune, which is the kind of value that survives review and then poisons every
// report it lands in.
func candidateTypeName(t webrtc.ICECandidateType) string {
	switch t {
	case webrtc.ICECandidateTypeHost:
		return "host"
	case webrtc.ICECandidateTypeSrflx:
		return "srflx"
	case webrtc.ICECandidateTypePrflx:
		return "prflx"
	case webrtc.ICECandidateTypeRelay:
		return "relay"
	default:
		return "unknown"
	}
}

// addrOf names a candidate as address, port and type. The type matters: a host candidate is the
// address the peer chose to advertise, while a peer-reflexive one is the address the network
// revealed whether the peer wanted it revealed or not - and only the first of those is a
// configuration anyone can be blamed for.
func addrOf(s webrtc.ICECandidateStats) string {
	return net.JoinHostPort(s.IP, strconv.Itoa(int(s.Port))) + "/" + candidateTypeName(s.CandidateType)
}

// candidate resolves a candidate id to its stats, which pion stores as either a value or a
// pointer depending on how the report was produced.
func candidate(rep webrtc.StatsReport, id string) (webrtc.ICECandidateStats, bool) {
	switch s := rep[id].(type) {
	case webrtc.ICECandidateStats:
		return s, true
	case *webrtc.ICECandidateStats:
		return *s, true
	default:
		return webrtc.ICECandidateStats{}, false
	}
}
