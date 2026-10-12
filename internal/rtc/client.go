package rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/lasttoss/rtc-media-bench/internal/ivf"
)

// Config is one headless participant.
type Config struct {
	URL      string        // ws://host/port/websocket
	Sample   string        // path to the VP8 .ivf this participant publishes
	Duration time.Duration // how long to stay in the room
	FPS      int
	// Index identifies this participant in filenames and in the track id it publishes.
	Index int
	// DumpDir, when set, writes the offers and answers this participant saw. Reading the SDP is
	// how a negotiation problem stops being a guess: the direction attributes are in there.
	DumpDir string
	// StatsDir, when set, writes the raw stats report. When a report is missing a number, the
	// first question is whether the report even contains it.
	StatsDir string
}

// Result is what one participant can honestly say about its own call.
//
// The timings are measured from the moment the dial starts and are kept apart on purpose: a call
// that dials fast, offers fast and then fails to find a path is a different problem from one that
// never gets an answer, and a single number would hide which of the two happened.
type Result struct {
	DialMS       float64 `json:"dial_ms"`
	OfferMS      float64 `json:"offer_ms"`
	IceMS        float64 `json:"ice_ms"`         // "join" here: ICE found a usable path
	FirstFrameMS float64 `json:"first_frame_ms"` // -1 when nobody else was publishing
	// DiedMS is when this participant's call ended, whichever way it ended: the duration running
	// out, or a signalling error. A fault that kills the server is measured by this number.
	DiedMS      float64 `json:"died_ms"`
	FramesSent  int     `json:"frames_sent"`
	Disconnects int     `json:"disconnects"`
	BitrateDown float64 `json:"bitrate_down_kbps"`
	BitrateUp   float64 `json:"bitrate_up_kbps"`
	Sample      Sample  `json:"sample"`
	Err         string  `json:"err,omitempty"`
	// LocalCandidates is every distinct address this participant offered. It is recorded because
	// the first version of this harness labelled it "remote" and asserted on it: a gate that
	// measures the wrong side of a connection passes whatever the other side does.
	LocalCandidates []string `json:"local_candidates,omitempty"`
}

// Run joins the room, publishes the sample at cfg.FPS, stays for cfg.Duration and reports.
func Run(ctx context.Context, cfg Config) Result {
	var res Result
	res.FirstFrameMS = -1
	if cfg.FPS <= 0 {
		cfg.FPS = 15
	}
	start := time.Now()
	ms := func() float64 { return float64(time.Since(start).Microseconds()) / 1000 }

	ws, _, err := websocket.DefaultDialer.DialContext(ctx, cfg.URL, nil)
	if err != nil {
		res.Err = "dial: " + err.Error()
		return res
	}
	defer func() { _ = ws.Close() }()
	res.DialMS = ms()

	// gorilla's websocket is not safe for concurrent writers and pion's callbacks fire from their
	// own goroutines, so every write goes through this.
	var wmu sync.Mutex
	write := func(event, data string) {
		wmu.Lock()
		defer wmu.Unlock()
		_ = ws.WriteJSON(map[string]string{"event": event, "data": data})
	}

	sample, err := ivf.Read(cfg.Sample)
	if err != nil {
		res.Err = err.Error()
		return res
	}

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		res.Err = "peer connection: " + err.Error()
		return res
	}
	defer func() { _ = pc.Close() }()

	var (
		cmu       sync.Mutex
		localSeen = map[string]struct{}{}
	)
	var disconnects atomic.Int64
	var iceOnce sync.Once
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		switch s {
		case webrtc.ICEConnectionStateConnected, webrtc.ICEConnectionStateCompleted:
			iceOnce.Do(func() { res.IceMS = ms() })
		case webrtc.ICEConnectionStateDisconnected, webrtc.ICEConnectionStateFailed:
			disconnects.Add(1)
		}
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		// These are *our* candidates, the ones we send. What the other side offers is in the stats,
		// not here, and confusing the two makes a gate about the SFU into a gate about the client.
		if init := c.ToJSON(); init.Candidate != "" {
			f := strings.Fields(init.Candidate)
			if len(f) >= 6 {
				addr := f[4] + ":" + f[5]
				cmu.Lock()
				if _, seen := localSeen[addr]; !seen {
					localSeen[addr] = struct{}{}
					res.LocalCandidates = append(res.LocalCandidates, addr)
				}
				cmu.Unlock()
			}
		}
		b, err := json.Marshal(c.ToJSON())
		if err != nil {
			return
		}
		write("candidate", string(b))
	})

	firstFrame := make(chan float64, 1)
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			for {
				if _, _, err := track.ReadRTP(); err != nil {
					return
				}
				select {
				case firstFrame <- ms():
				default:
				}
			}
		}()
	})

	// The id has to be unique per participant. Two publishers sharing a track id collapse into a
	// single entry in any map keyed by id - including the SFU's - and the room then has media that
	// nobody forwards: every participant uploads, nobody receives. It is a silent failure, so the
	// uniqueness is written down here rather than left to whoever writes the next caller.
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		fmt.Sprintf("video-%d", cfg.Index), fmt.Sprintf("bench-%d", cfg.Index))
	if err != nil {
		res.Err = "track: " + err.Error()
		return res
	}
	if _, err := pc.AddTrack(track); err != nil {
		res.Err = "add track: " + err.Error()
		return res
	}

	// The SFU offers first, and offers again whenever somebody publishes: renegotiation is the
	// normal case here, not an edge case. A client that answers only the first offer appears to
	// work with one participant and never receives anybody else's media.
	var offers int
	readErr := make(chan error, 1)
	go func() {
		for {
			_, raw, err := ws.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
			var msg struct {
				Event string `json:"event"`
				Data  string `json:"data"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			switch msg.Event {
			case "offer":
				offers++
				if offers == 1 {
					res.OfferMS = ms()
				}
				var offer webrtc.SessionDescription
				if err := json.Unmarshal([]byte(msg.Data), &offer); err != nil {
					continue
				}
				if cfg.DumpDir != "" {
					_ = os.WriteFile(filepath.Join(cfg.DumpDir, fmt.Sprintf("offer-%d-%d.sdp", cfg.Index, offers)),
						[]byte(offer.SDP), 0o600)
				}
				if err := pc.SetRemoteDescription(offer); err != nil {
					readErr <- err
					return
				}
				answer, err := pc.CreateAnswer(nil)
				if err != nil {
					readErr <- err
					return
				}
				if err := pc.SetLocalDescription(answer); err != nil {
					readErr <- err
					return
				}
				if cfg.DumpDir != "" {
					_ = os.WriteFile(filepath.Join(cfg.DumpDir, fmt.Sprintf("answer-%d-%d.sdp", cfg.Index, offers)),
						[]byte(answer.SDP), 0o600)
				}
				b, err := json.Marshal(answer)
				if err != nil {
					readErr <- err
					return
				}
				write("answer", string(b))
			case "candidate":
				var cand webrtc.ICECandidateInit
				if err := json.Unmarshal([]byte(msg.Data), &cand); err != nil {
					continue
				}
				if err := pc.AddICECandidate(cand); err != nil {
					continue
				}
			}
		}
	}()

	runCtx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()
	frameDur := time.Second / time.Duration(cfg.FPS)
	var sent atomic.Int64
	go func() {
		for {
			for _, f := range sample {
				select {
				case <-runCtx.Done():
					return
				default:
				}
				if err := track.WriteSample(media.Sample{Data: f.Data, Duration: frameDur}); err != nil {
					return
				}
				sent.Add(1)
				time.Sleep(frameDur)
			}
		}
	}()

	select {
	case <-runCtx.Done():
	case err := <-readErr:
		res.Err = "signalling: " + err.Error()
	}
	res.DiedMS = ms()

	select {
	case t := <-firstFrame:
		res.FirstFrameMS = t
	default:
	}

	res.FramesSent = int(sent.Load())
	res.Disconnects = int(disconnects.Load())
	report := pc.GetStats()
	if cfg.StatsDir != "" {
		if b, err := json.MarshalIndent(report, "", " "); err == nil {
			_ = os.WriteFile(filepath.Join(cfg.StatsDir, fmt.Sprintf("stats-%d.json", cfg.Index)), b, 0o600)
		}
	}
	res.Sample = Extract(report)
	secs := cfg.Duration.Seconds()
	res.BitrateDown = float64(res.Sample.BytesReceived*8) / secs / 1000
	res.BitrateUp = float64(res.Sample.BytesSent*8) / secs / 1000
	return res
}
