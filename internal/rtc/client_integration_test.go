package rtc_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lasttoss/rtc-media-bench/internal/rtc"
)

// The harness's job is to bring up real peer connections, so the only test that proves it works is one
// that does. This runs the vendored SFU from the working tree and puts two participants through it.
//
// It skips unless RTC_BENCH_SFU names a built SFU binary, because a test that shells out to a compiler
// is a test that fails for reasons unrelated to the code. `make test-integration` builds it first.
func TestTwoParticipantsExchangeMedia(t *testing.T) {
	sfu := os.Getenv("RTC_BENCH_SFU")
	if sfu == "" {
		t.Skip("set RTC_BENCH_SFU to a built lab/sfu binary (make test-integration)")
	}
	if _, err := os.Stat(sfu); err != nil {
		t.Fatalf("RTC_BENCH_SFU=%s: %v", sfu, err)
	}

	ws := freePort(t)
	mux := freePort(t)

	// The SFU loads its landing page from the working directory at start-up, which is how a container
	// built without index.html panics before it serves anything.
	// The SFU's own output goes to a file rather than to /dev/null, which is where an
	// exec.Command that sets no Stdout sends it. A run that fails with "close 1006" and no
	// reason is a run nobody can act on; the file is printed when this test fails.
	sfuLog, err := os.CreateTemp(t.TempDir(), "sfu-*.log")
	if err != nil {
		t.Fatalf("create the sfu's log: %v", err)
	}
	cmd := exec.Command(sfu, fmt.Sprintf("-addr=:%d", ws), fmt.Sprintf("-udp-mux-port=%d", mux),
		"-advertise-ip=127.0.0.1")
	cmd.Dir = filepath.Join("..", "..", "lab", "sfu")
	cmd.Stdout, cmd.Stderr = sfuLog, sfuLog
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sfu: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	_ = sfuLog.Close() // the child holds its own descriptor from here on
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
		}
		if !t.Failed() {
			return
		}
		body, _ := os.ReadFile(sfuLog.Name())
		t.Logf("the sfu's own output, which a failing run needs:\n%s", body)
	})
	waitForPort(t, ws)

	url := fmt.Sprintf("ws://127.0.0.1:%d/websocket", ws)
	sample := filepath.Join("..", "..", "testdata", "sample.ivf")

	// Both participants at the same time. Run one after the other and they never overlap, so nobody
	// has anybody to receive from - which is exactly what the first version of this test did, and it
	// reported the harness as broken when the harness was right.
	results := make([]rtc.Result, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = rtc.Run(context.Background(), rtc.Config{
				URL: url, Sample: sample, Duration: 6 * time.Second, FPS: 15, Index: i})
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(40 * time.Second):
		t.Fatal("the participants never finished")
	}

	// A participant reporting "close 1006" is a symptom; the cause is usually the SFU having
	// died, and saying so here is the difference between a diagnosis and a riddle.
	select {
	case err := <-exited:
		body, _ := os.ReadFile(sfuLog.Name())
		exited <- err
		t.Fatalf("the sfu exited while the participants were joining (%v); its output was:\n%s", err, body)
	default:
	}

	for i, r := range results {
		if r.Err != "" {
			t.Errorf("participant %d: %s", i, r.Err)
		}
		if r.IceMS <= 0 {
			t.Errorf("participant %d never reached ICE connected (dial %v ms, offer %v ms)",
				i, r.DialMS, r.OfferMS)
		}
		if r.FramesSent == 0 {
			t.Errorf("participant %d published nothing", i)
		}
	}

	// The second participant is published before the first one joins, so somebody has to receive
	// somebody: that is the property a track id shared between participants silently breaks.
	var received uint32
	for _, r := range results {
		received += r.Sample.InboundReceived
	}
	if received == 0 {
		t.Fatalf("no participant received media: %+v", results)
	}
	t.Logf("join %v/%v ms, %d packets received, %d frames published",
		results[0].IceMS, results[1].IceMS, received, results[0].FramesSent+results[1].FramesSent)
}

func freePort(t *testing.T) int {
	t.Helper()
	conn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	return conn.Addr().(*net.TCPAddr).Port
}

func waitForPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the SFU never listened on %d", port)
}
