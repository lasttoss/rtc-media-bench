package rtc

import (
	"math"
	"testing"

	"github.com/pion/webrtc/v4"
)

// Fixture built the way pion builds a report: typed structs keyed by id.
func fixture() webrtc.StatsReport {
	return webrtc.StatsReport{
		"inbound": webrtc.InboundRTPStreamStats{
			PacketsReceived: 900, PacketsLost: 100, Jitter: 0.02, BytesReceived: 450_000,
		},
		"inbound-2": webrtc.InboundRTPStreamStats{
			PacketsReceived: 100, PacketsLost: 0, Jitter: 0.005, BytesReceived: 50_000,
		},
		"remote-inbound": webrtc.RemoteInboundRTPStreamStats{
			PacketsReceived: 480, PacketsLost: 20, RoundTripTime: 0.043, FractionLost: 0.04,
		},
		"pair": webrtc.ICECandidatePairStats{
			State: webrtc.StatsICECandidatePairStateSucceeded, Nominated: true, CurrentRoundTripTime: 0.011,
		},
		"pair-not-nominated": webrtc.ICECandidatePairStats{
			State: webrtc.StatsICECandidatePairStateSucceeded, CurrentRoundTripTime: 9.9,
		},
	}
}

func TestExtractSumsAndConverts(t *testing.T) {
	got := Extract(fixture())
	if got.InboundReceived != 1000 || got.InboundLost != 100 {
		t.Errorf("inbound = %d/%d, want 1000/100", got.InboundReceived, got.InboundLost)
	}
	if math.Abs(got.JitterMS-20) > 1e-9 {
		t.Errorf("jitter = %v ms, want 20 (the larger of the two streams)", got.JitterMS)
	}
	if got.UploadSent != 500 || got.UploadLost != 20 {
		t.Errorf("upload = %d sent/%d lost, want 500/20", got.UploadSent, got.UploadLost)
	}
	if math.Abs(got.UploadRTTMS-43) > 1e-9 {
		t.Errorf("upload rtt = %v ms, want 43", got.UploadRTTMS)
	}
	// Only the nominated succeeded pair counts: a losing candidate pair carries no media, and
	// its RTT is a number about a path that is not being used.
	if math.Abs(got.IceRTTMS-11) > 1e-9 {
		t.Errorf("ice rtt = %v ms, want 11 (nominated pair only)", got.IceRTTMS)
	}
	if got.BytesReceived != 500_000 {
		t.Errorf("bytes = %d, want 500000", got.BytesReceived)
	}
	if math.Abs(got.FractionLost-0.04) > 1e-9 {
		t.Errorf("fraction lost = %v, want 0.04", got.FractionLost)
	}
}

func TestExtractOnAnEmptyReportIsZero(t *testing.T) {
	got := Extract(webrtc.StatsReport{})
	// Sample has a slice field, so it is not comparable: every scalar is checked instead, and the
	// advertised list must be empty rather than nil-but-populated.
	if got.InboundReceived != 0 || got.UploadSent != 0 || got.IceRTTMS != 0 ||
		got.RemoteIP != "" || len(got.Advertised) != 0 {
		t.Fatalf("empty report produced %+v", got)
	}
}

// A negative cumulative loss is legal on the wire and must not become a huge unsigned number.
func TestExtractClampsNegativeLoss(t *testing.T) {
	got := Extract(webrtc.StatsReport{
		"remote-inbound": webrtc.RemoteInboundRTPStreamStats{PacketsReceived: 10, PacketsLost: -3},
	})
	if got.UploadLost != 0 {
		t.Fatalf("upload lost = %d, want 0", got.UploadLost)
	}
	if got.UploadSent != 10 {
		t.Fatalf("upload sent = %d, want 10 (the negative count is not a packet)", got.UploadSent)
	}
}

// The pair's two ids are the only way stats names an address, and the advertised list is what a
// slow join has to be explained by: every interface a peer owns, whether or not it is reachable.
func TestExtractResolvesCandidatesThroughThePair(t *testing.T) {
	rep := webrtc.StatsReport{
		"pair": webrtc.ICECandidatePairStats{
			State: webrtc.StatsICECandidatePairStateSucceeded, Nominated: true,
			CurrentRoundTripTime: 0.011, BytesSent: 4096,
			LocalCandidateID: "local", RemoteCandidateID: "remote",
		},
		"local":  webrtc.ICECandidateStats{ID: "local", IP: "10.0.0.5", Port: 40000, CandidateType: webrtc.ICECandidateTypeHost},
		"remote": webrtc.ICECandidateStats{ID: "remote", IP: "203.0.113.9", Port: 50000, CandidateType: webrtc.ICECandidateTypeSrflx},
		"unused": webrtc.ICECandidateStats{ID: "unused", IP: "127.0.0.1", Port: 1},
	}
	got := Extract(rep)
	if got.LocalIP != "10.0.0.5" || got.LocalPort != 40000 {
		t.Errorf("local = %s:%d", got.LocalIP, got.LocalPort)
	}
	if got.RemoteIP != "203.0.113.9" || got.RemotePort != 50000 {
		t.Errorf("remote = %s:%d", got.RemoteIP, got.RemotePort)
	}
	if got.RemoteCandidateType != "srflx" {
		t.Errorf("remote type = %q, want srflx", got.RemoteCandidateType)
	}
	if got.BytesSent != 4096 {
		t.Errorf("bytes sent = %d, want 4096", got.BytesSent)
	}
	// A candidate that no pair references is neither this side's nor the peer's: it is in the
	// report but not in use, and counting it would make a one-interface peer look like a
	// twenty-interface one.
	if len(got.Advertised) != 1 || got.Advertised[0] != "203.0.113.9:50000/srflx" {
		t.Errorf("advertised = %v, want only the candidate a pair points at", got.Advertised)
	}
}

// The numeric candidate type is not a string, and printing it as one gives a rune. This is the
// test that would have caught that: the value has to be a word.
func TestCandidateTypeIsSpelledOut(t *testing.T) {
	for want, in := range map[string]webrtc.ICECandidateType{
		"host": webrtc.ICECandidateTypeHost, "srflx": webrtc.ICECandidateTypeSrflx,
		"prflx": webrtc.ICECandidateTypePrflx, "relay": webrtc.ICECandidateTypeRelay,
	} {
		if got := candidateTypeName(in); got != want {
			t.Errorf("candidateTypeName(%d) = %q, want %q", in, got, want)
		}
	}
	if got := candidateTypeName(webrtc.ICECandidateType(99)); got != "unknown" {
		t.Errorf("an unknown type = %q, want unknown", got)
	}
}
