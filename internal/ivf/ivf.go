// Package ivf reads the tiny VP8 sample this harness publishes.
//
// IVF is the container the VP8 reference encoder writes: a 32-byte header, then one
// (size, timestamp, payload) record per frame. The payload is exactly what an RTP packet
// carries, which is why the sample can be written into a static sample track without a codec.
package ivf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

const headerSize = 32

// Frame is one encoded frame from the file.
type Frame struct {
	Data      []byte
	Timestamp uint64
}

// Read parses an IVF file. It refuses anything that is not a VP8 IVF, because a silently
// mis-parsed sample produces a track that publishes bytes nothing can decode - a failure that
// looks like a network problem from the outside.
func Read(path string) ([]Frame, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < headerSize {
		return nil, fmt.Errorf("%s: %d bytes is shorter than the %d-byte IVF header", path, len(raw), headerSize)
	}
	if string(raw[0:4]) != "DKIF" {
		return nil, fmt.Errorf("%s: not an IVF file (magic %q)", path, raw[0:4])
	}
	if fourcc := string(raw[8:12]); fourcc != "VP80" {
		return nil, fmt.Errorf("%s: codec %q, this harness publishes VP8 only", path, fourcc)
	}

	frames := make([]Frame, 0, binary.LittleEndian.Uint32(raw[24:28]))
	for off := headerSize; off+12 <= len(raw); {
		size := int(binary.LittleEndian.Uint32(raw[off : off+4]))
		ts := binary.LittleEndian.Uint64(raw[off+4 : off+12])
		off += 12
		if size < 0 || off+size > len(raw) {
			return nil, fmt.Errorf("%s: frame at %d claims %d bytes, %d remain", path, off, size, len(raw)-off)
		}
		frames = append(frames, Frame{Data: raw[off : off+size], Timestamp: ts})
		off += size
	}
	if len(frames) == 0 {
		return nil, errors.New(path + ": no frames")
	}
	return frames, nil
}
