//go:build darwin && cgo

package commands

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrayIconPNGIsValidTemplateImage(t *testing.T) {
	raw := trayIconPNG()
	require.NotEmpty(t, raw)

	img, err := png.Decode(bytes.NewReader(raw))
	require.NoError(t, err)

	bounds := img.Bounds()
	assert.Equal(t, trayIconSize, bounds.Dx())
	assert.Equal(t, trayIconSize, bounds.Dy())

	// A template icon carries its shape in the alpha channel: some pixels must be
	// opaque (the clock) and some fully transparent (the background).
	var opaque, transparent int
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			switch a {
			case 0:
				transparent++
			case 0xFFFF:
				opaque++
			}
		}
	}
	assert.Positive(t, opaque, "clock shape should have opaque pixels")
	assert.Positive(t, transparent, "background should be transparent")
}
