package report

import (
	"math"
	"strings"
	"testing"
)

func TestPercentileIsNearestRank(t *testing.T) {
	values := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	for _, tc := range []struct {
		p    float64
		want float64
	}{
		{0, 10}, {50, 50}, {95, 100}, {100, 100}, {11, 20}, {89, 90},
	} {
		if got := Percentile(values, tc.p); got != tc.want {
			t.Errorf("Percentile(p%v) = %v, want %v", tc.p, got, tc.want)
		}
	}
}

// A p95 is a value a participant actually experienced: with four samples and p95, that is the
// largest one, not an interpolation towards it.
func TestPercentileDoesNotInventValues(t *testing.T) {
	values := []float64{12, 14, 16, 900}
	got := Percentile(values, 95)
	if got != 900 {
		t.Fatalf("p95 = %v, want 900 (a measured value)", got)
	}
	seen := false
	for _, v := range values {
		if v == got {
			seen = true
		}
	}
	if !seen {
		t.Fatal("the percentile is not one of the inputs")
	}
}

func TestPercentileOnNothingIsZeroNotNaN(t *testing.T) {
	got := Percentile(nil, 95)
	if got != 0 || math.IsNaN(got) {
		t.Fatalf("empty input gave %v, want 0", got)
	}
	if got := Percentile([]float64{7}, 95); got != 7 {
		t.Fatalf("single sample gave %v, want 7", got)
	}
}

// Missing data has to look missing: a join time of zero is "we never connected", and the table
// says n/a rather than printing 0.0 ms, which reads like a very fast join.
func TestTextSaysWhenThereIsNothingToReport(t *testing.T) {
	out := Summary{Participants: 1, Failed: []string{"dial: connection refused"}}.Text()
	if !strings.Contains(out, "n/a") {
		t.Errorf("a report with no timings must say n/a:\n%s", out)
	}
	if !strings.Contains(out, "nobody else was publishing") {
		t.Errorf("a report with no inbound media must say why:\n%s", out)
	}
	if !strings.Contains(out, "dial: connection refused") {
		t.Errorf("the failure must be in the report:\n%s", out)
	}
	if strings.Contains(out, "0.0 ms") {
		t.Errorf("zero must not be printed as a timing:\n%s", out)
	}
}

func TestTextCarriesTheNumbers(t *testing.T) {
	out := Summary{
		Participants: 3,
		Join:         []float64{100, 200, 300},
		FirstFrame:   []float64{400, 500, 600},
		RTT:          []float64{10, 11, 12},
		InboundRecv:  1000, InboundLost: 3, UploadSent: 2000, UploadLost: 1, Disconnects: 2,
	}.Text()
	for _, want := range []string{"join              p50 200.0 ms", "first frame       p50 500.0 ms",
		"rtt               p50 11.0 ms", "inbound 1000 received / 3 lost", "upload 2000 sent / 1 lost",
		"disconnects       2"} {
		if !strings.Contains(out, want) {
			t.Errorf("table is missing %q:\n%s", want, out)
		}
	}
}

// No receiver reports means the sender-side loss number is unknown, and unknown must not print as
// zero: "0 lost" is a claim about the network, "n/a" is a statement about the measurement.
func TestUploadWithoutReceiverReportsSaysSoInsteadOfZero(t *testing.T) {
	out := Summary{Participants: 1, FramesSent: 300, UploadSent: 0, InboundRecv: 10}.Text()
	if !strings.Contains(out, "upload n/a") {
		t.Fatalf("missing upload stats must be reported as n/a:\n%s", out)
	}
	if strings.Contains(out, "0 sent / 0 lost") {
		t.Fatalf("a missing upload must not print as zero:\n%s", out)
	}
	out2 := Summary{Participants: 1, FramesSent: 300, UploadSent: 250, UploadLost: 4}.Text()
	if !strings.Contains(out2, "upload 250 sent / 4 lost") {
		t.Fatalf("a present upload must be printed:\n%s", out2)
	}
}
