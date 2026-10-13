// Command bench joins a room with N headless participants and reports what happened.
//
// It publishes the VP8 sample in testdata/ and measures only what a participant can see from the
// inside: how long the join took, the RTT of the path media actually uses, what the SFU says
// arrived from it, and how much traffic it cost. Nothing here needs a cloud service or a key.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/lasttoss/rtc-media-bench/internal/report"
	"github.com/lasttoss/rtc-media-bench/internal/rtc"
)

func main() {
	var (
		url      = flag.String("url", "ws://127.0.0.1:17880/websocket", "SFU websocket signalling address")
		sample   = flag.String("sample", "testdata/sample.ivf", "VP8 .ivf published by every participant")
		clients  = flag.Int("clients", 3, "how many participants to join")
		duration = flag.Duration("duration", 10*time.Second, "how long to stay in the room")
		stagger  = flag.Duration("stagger", 250*time.Millisecond, "delay between joins")
		fps      = flag.Int("fps", 15, "publish rate")
		asJSON   = flag.Bool("json", false, "print the summary as JSON")
		dump     = flag.String("dump", "", "directory to write the offer/answer SDP of every participant into")
		statsDir = flag.String("stats-dir", "", "directory to write the raw stats report of every participant into")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	results := make([]rtc.Result, *clients)
	start := time.Now()
	var wg sync.WaitGroup
	for i := range *clients {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Stagger the joins: everyone dialling in the same millisecond measures a thundering
			// herd, which is a real thing to measure but not the join time of one participant.
			select {
			case <-time.After(time.Duration(i) * *stagger):
			case <-ctx.Done():
				results[i].Err = "cancelled"
				return
			}
			results[i] = rtc.Run(ctx, rtc.Config{
				URL: *url, Sample: *sample, Duration: *duration, FPS: *fps,
				Index: i, DumpDir: *dump, StatsDir: *statsDir,
			})
		}(i)
	}
	wg.Wait()

	sum := summarise(results, *clients).WithDetail(details(results))
	_ = start

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(sum); err != nil {
			fmt.Fprintln(os.Stderr, "encode:", err)
			os.Exit(1)
		}
	} else {
		fmt.Print(sum.Text())
	}
	// No failures and nobody connected is not success: it is a fault that the harness did not
	// classify as an error, and a lab script that treats it as success gates nothing.
	if len(sum.Failed) > 0 || (len(sum.Join) == 0 && sum.Participants > 0) {
		os.Exit(1)
	}
}

func details(results []rtc.Result) []report.Detail {
	out := make([]report.Detail, 0, len(results))
	for i, r := range results {
		out = append(out, report.Detail{
			Index: i, DialMS: r.DialMS, OfferMS: r.OfferMS, IceMS: r.IceMS,
			FirstFrameMS: r.FirstFrameMS, Advertised: r.Sample.Advertised, LocalIP: r.Sample.LocalIP,
			LocalPort: r.Sample.LocalPort, RemoteIP: r.Sample.RemoteIP, RemotePort: r.Sample.RemotePort,
			RemoteType: r.Sample.RemoteCandidateType,
		})
	}
	return out
}

func summarise(results []rtc.Result, want int) report.Summary {
	sum := report.Summary{Participants: want}
	for i, r := range results {
		if r.Err != "" {
			// The failure is recorded and the measurements are kept. A participant that joined, was
			// in a call and then had the server taken away is the evidence a fault lab is built on;
			// dropping its numbers because it ended in an error throws that evidence away.
			sum.Failed = append(sum.Failed, fmt.Sprintf("participant %d: %s", i, r.Err))
		}
		if r.DiedMS > 0 {
			sum.Survived = append(sum.Survived, r.DiedMS)
		}
		if r.IceMS > 0 {
			sum.Join = append(sum.Join, r.IceMS)
		}
		if r.FirstFrameMS > 0 {
			sum.FirstFrame = append(sum.FirstFrame, r.FirstFrameMS)
		}
		if r.Sample.IceRTTMS > 0 {
			sum.RTT = append(sum.RTT, r.Sample.IceRTTMS)
		}
		if r.Sample.UploadRTTMS > 0 {
			sum.UploadRTT = append(sum.UploadRTT, r.Sample.UploadRTTMS)
		}
		if r.Sample.JitterMS > 0 {
			sum.Jitter = append(sum.Jitter, r.Sample.JitterMS)
		}
		if r.BitrateDown > 0 {
			sum.DownKbps = append(sum.DownKbps, r.BitrateDown)
		}
		if r.BitrateUp > 0 {
			sum.UpKbps = append(sum.UpKbps, r.BitrateUp)
		}
		sum.FramesSent += r.FramesSent
		sum.InboundRecv += r.Sample.InboundReceived
		sum.InboundLost += r.Sample.InboundLost
		sum.UploadSent += r.Sample.UploadSent
		sum.UploadLost += r.Sample.UploadLost
		sum.Disconnects += r.Disconnects
	}
	return sum
}
