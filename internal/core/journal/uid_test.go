package journal_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
)

func TestNewUID(t *testing.T) {
	now := time.UnixMilli(1794985200000)

	uid, err := journal.NewUID(now, bytes.NewReader(bytes.Repeat([]byte{0xff}, 10)))
	require.NoError(t, err)
	assert.Equal(t, "01a1ed74-8980-7fff-bfff-ffffffffffff", uid, "time, version 7, variant 10")

	uid, err = journal.NewUID(now, bytes.NewReader(make([]byte, 10)))
	require.NoError(t, err)
	assert.Equal(t, "01a1ed74-8980-7000-8000-000000000000", uid)

	// A change naming a generated uid is valid.
	uid, err = journal.NewUID(now, rand.Reader)
	require.NoError(t, err)
	line, err := json.Marshal(map[string]any{
		"v": 1, "hlc": "0001794985200000.000000", "dev": "7f3a9c2e5b1d4a60", "type": "activity", "uid": uid,
		"set": map[string]any{"project": "Ops"},
	})
	require.NoError(t, err)
	_, err = journal.ParseChange(line)
	require.NoError(t, err)

	later, err := journal.NewUID(now.Add(time.Millisecond), rand.Reader)
	require.NoError(t, err)
	assert.Less(t, uid, later, "uids made later sort later")

	_, err = journal.NewUID(now, bytes.NewReader(nil))
	require.Error(t, err)
}
