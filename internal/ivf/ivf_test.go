package ivf

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func build(t *testing.T, magic, fourcc string, frames [][]byte) string {
	t.Helper()
	out := make([]byte, 32)
	copy(out[0:4], magic)
	copy(out[8:12], fourcc)
	binary.LittleEndian.PutUint16(out[12:14], 160)
	binary.LittleEndian.PutUint16(out[14:16], 120)
	binary.LittleEndian.PutUint32(out[24:28], uint32(len(frames)))
	for i, f := range frames {
		var rec [12]byte
		binary.LittleEndian.PutUint32(rec[0:4], uint32(len(f)))
		binary.LittleEndian.PutUint64(rec[4:12], uint64(i))
		out = append(out, rec[:]...)
		out = append(out, f...)
	}
	path := filepath.Join(t.TempDir(), "sample.ivf")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadTakesTheFramesApart(t *testing.T) {
	path := build(t, "DKIF", "VP80", [][]byte{{1, 2, 3}, {4}, {5, 6}})
	frames, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
	if got := string(frames[0].Data); got != "\x01\x02\x03" {
		t.Errorf("frame 0 = %q", got)
	}
	if frames[2].Timestamp != 2 {
		t.Errorf("frame 2 timestamp = %d, want 2", frames[2].Timestamp)
	}
}

// A wrong magic, a wrong codec and a lying frame size all have to be refused: each of them
// produces a publisher that looks healthy and sends bytes nobody can decode.
func TestReadRefusesFilesThatWouldPublishGarbage(t *testing.T) {
	good := build(t, "DKIF", "VP80", [][]byte{{1, 2, 3}})

	notIVF := build(t, "OggS", "VP80", [][]byte{{1}})
	notVP8 := build(t, "DKIF", "AV01", [][]byte{{1}})

	short := filepath.Join(t.TempDir(), "short.ivf")
	if err := os.WriteFile(short, []byte("DKIF"), 0o600); err != nil {
		t.Fatal(err)
	}

	truncated := build(t, "DKIF", "VP80", [][]byte{{1, 2, 3}})
	raw, err := os.ReadFile(truncated)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(raw[32:36], 9999) // claims more than the file holds
	lying := filepath.Join(t.TempDir(), "lying.ivf")
	if err := os.WriteFile(lying, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"not an IVF file": notIVF, "not VP8": notVP8, "too short": short, "size lies": lying,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Read(path); err == nil {
				t.Fatalf("Read(%s) accepted a file it should have refused", name)
			}
		})
	}

	if _, err := Read(good); err != nil {
		t.Fatalf("the good file must still pass: %v", err)
	}
	if _, err := Read(filepath.Join(t.TempDir(), "missing.ivf")); err == nil {
		t.Fatal("a missing file must be an error")
	}
}
