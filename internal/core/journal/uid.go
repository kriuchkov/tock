package journal

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"time"

	"github.com/go-faster/errors"
)

// NewUID returns a new record uid: a lowercase UUIDv7 that starts with now in Unix milliseconds, so
// uids made later sort later, followed by random bits read from entropy (crypto/rand.Reader in tock).
func NewUID(now time.Time, entropy io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(entropy, b[6:]); err != nil {
		return "", errors.Wrap(err, "read random bits")
	}
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(now.UnixMilli()))
	copy(b[:6], ms[2:])
	b[6] = b[6]&0x0f | 0x70 // version 7
	b[8] = b[8]&0x3f | 0x80 // RFC 9562 variant

	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
