//go:build darwin

package commands

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Menu bar template icon geometry. A macOS status item slot is ~22pt, so the
// image is rendered at @2x (retina) and masked by its alpha channel — macOS
// ignores the color and recolors the shape for the current appearance.
//
// The shape mirrors the Tock logo: three stacked rounded bars with a downward
// arrow piercing the stack.
const (
	trayIconSize  = 44
	trayAAFeather = 0.5

	trayBarCenterX  = 22.0
	trayBarHalfW    = 16.5
	trayBarHalfH    = 5.5
	trayBarRadius   = 3.0
	trayBarStroke   = 2.0
	trayArrowStroke = 2.4
)

// trayBarCentersY are the vertical centers of the three stacked bars.
var trayBarCentersY = [...]float64{9.0, 22.0, 35.0}

// trayIconPNG renders the monochrome Tock mark used as the menu bar template
// icon. Drawn pixels are black with coverage-based alpha for smooth edges.
func trayIconPNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, trayIconSize, trayIconSize))

	plot := func(x, y int, coverage float64) {
		if x < 0 || y < 0 || x >= trayIconSize || y >= trayIconSize {
			return
		}
		coverage = math.Max(0, math.Min(1, coverage))
		alpha := uint8(coverage * 255)
		if alpha <= img.NRGBAAt(x, y).A {
			return
		}
		img.SetNRGBA(x, y, color.NRGBA{A: alpha})
	}

	for _, cy := range trayBarCentersY {
		drawRoundedRectOutline(plot, trayBarCenterX, cy, trayBarHalfW, trayBarHalfH, trayBarRadius, trayBarStroke)
	}

	// Play triangle in the top bar (left side), echoing the logo.
	drawTrayTriangle(plot, 9.5, 6.3, 9.5, 11.7, 15.0, 9.0)

	// Arrow through the stack: shaft plus a downward chevron head.
	drawTraySegment(plot, trayBarCenterX, 14.5, trayBarCenterX, 35.0, trayArrowStroke)
	drawTraySegment(plot, trayBarCenterX, 37.0, trayBarCenterX-5.5, 31.5, trayArrowStroke)
	drawTraySegment(plot, trayBarCenterX, 37.0, trayBarCenterX+5.5, 31.5, trayArrowStroke)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// drawRoundedRectOutline strokes the outline of a rounded rectangle using its
// signed distance field, so edges are anti-aliased.
func drawRoundedRectOutline(plot func(x, y int, coverage float64), cx, cy, halfW, halfH, radius, thick float64) {
	half := thick / 2
	minX := int(math.Floor(cx - halfW - thick))
	maxX := int(math.Ceil(cx + halfW + thick))
	minY := int(math.Floor(cy - halfH - thick))
	maxY := int(math.Ceil(cy + halfH + thick))

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			sdf := roundedRectSDF(float64(x), float64(y), cx, cy, halfW, halfH, radius)
			plot(x, y, half-math.Abs(sdf)+trayAAFeather)
		}
	}
}

// roundedRectSDF returns the signed distance from a point to a rounded
// rectangle boundary (negative inside, positive outside).
func roundedRectSDF(px, py, cx, cy, halfW, halfH, radius float64) float64 {
	dx := math.Abs(px-cx) - (halfW - radius)
	dy := math.Abs(py-cy) - (halfH - radius)
	outside := math.Hypot(math.Max(dx, 0), math.Max(dy, 0))
	inside := math.Min(math.Max(dx, dy), 0)
	return outside + inside - radius
}

// drawTrayTriangle fills a triangle with anti-aliased edges via its signed
// distance field.
func drawTrayTriangle(plot func(x, y int, coverage float64), ax, ay, bx, by, cx, cy float64) {
	minX := int(math.Floor(math.Min(ax, math.Min(bx, cx)) - 1))
	maxX := int(math.Ceil(math.Max(ax, math.Max(bx, cx)) + 1))
	minY := int(math.Floor(math.Min(ay, math.Min(by, cy)) - 1))
	maxY := int(math.Ceil(math.Max(ay, math.Max(by, cy)) + 1))

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			plot(x, y, -triangleSDF(float64(x), float64(y), ax, ay, bx, by, cx, cy)+trayAAFeather)
		}
	}
}

// triangleSDF returns the signed distance from a point to a triangle (negative
// inside), after Inigo Quilez's sdTriangle.
func triangleSDF(px, py, ax, ay, bx, by, cx, cy float64) float64 {
	e0x, e0y := bx-ax, by-ay
	e1x, e1y := cx-bx, cy-by
	e2x, e2y := ax-cx, ay-cy
	v0x, v0y := px-ax, py-ay
	v1x, v1y := px-bx, py-by
	v2x, v2y := px-cx, py-cy

	clamp01 := func(t float64) float64 { return math.Max(0, math.Min(1, t)) }
	pq0x, pq0y := edgeDistance(v0x, v0y, e0x, e0y, clamp01)
	pq1x, pq1y := edgeDistance(v1x, v1y, e1x, e1y, clamp01)
	pq2x, pq2y := edgeDistance(v2x, v2y, e2x, e2y, clamp01)

	s := math.Copysign(1, e0x*e2y-e0y*e2x)
	dist, sign := pq0x*pq0x+pq0y*pq0y, s*(v0x*e0y-v0y*e0x)
	if d := pq1x*pq1x + pq1y*pq1y; d < dist {
		dist, sign = d, s*(v1x*e1y-v1y*e1x)
	}
	if d := pq2x*pq2x + pq2y*pq2y; d < dist {
		dist, sign = d, s*(v2x*e2y-v2y*e2x)
	}
	return -math.Sqrt(dist) * math.Copysign(1, sign)
}

// edgeDistance returns the vector from a point to the nearest spot on an edge.
func edgeDistance(vx, vy, ex, ey float64, clamp01 func(float64) float64) (float64, float64) {
	t := clamp01((vx*ex + vy*ey) / (ex*ex + ey*ey))
	return vx - ex*t, vy - ey*t
}

func drawTraySegment(plot func(x, y int, coverage float64), x0, y0, x1, y1, thick float64) {
	minX := int(math.Floor(math.Min(x0, x1) - thick))
	maxX := int(math.Ceil(math.Max(x0, x1) + thick))
	minY := int(math.Floor(math.Min(y0, y1) - thick))
	maxY := int(math.Ceil(math.Max(y0, y1) + thick))
	half := thick / 2

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			d := distanceToSegment(float64(x), float64(y), x0, y0, x1, y1)
			plot(x, y, half-d+trayAAFeather)
		}
	}
}

func distanceToSegment(px, py, x0, y0, x1, y1 float64) float64 {
	dx, dy := x1-x0, y1-y0
	if dx == 0 && dy == 0 {
		return math.Hypot(px-x0, py-y0)
	}
	t := ((px-x0)*dx + (py-y0)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(x0+t*dx), py-(y0+t*dy))
}
