package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"

	"golang.org/x/image/vector"
)

type point struct{ x, y float64 }

// trayIcon draws the app's mark for the menu bar: a prism, the beam going
// in and the spectrum coming out, black on transparent for macOS to tint
// as the menu bar is. It is drawn on a 24-unit grid, as the SVG in
// design/icons/menubar-f1.svg.
func trayIcon() []byte {
	const size = 32
	s := float64(size) / 24
	r := vector.NewRasterizer(size, size)
	scale := func(ps []point) []point {
		out := make([]point, len(ps))
		for i, p := range ps {
			out[i] = point{p.x * s, p.y * s}
		}
		return out
	}

	// The prism's outline: the triangle grown and shrunk by half the stroke
	// about its incenter, the inner one wound the other way to cut it out.
	tri := []point{{9.5, 4.5}, {14.7, 14.7}, {4.3, 14.7}}
	center, inradius := incircle(tri)
	const stroke = 1.8
	fill(r, scale(about(tri, center, (inradius+stroke/2)/inradius)), true)
	fill(r, scale(about(tri, center, (inradius-stroke/2)/inradius)), false)
	// The beam going in, and the spectrum fanning out.
	fill(r, scale(thick(point{1.5, 12.6}, point{6.5, 11.6}, 1.6)), true)
	fill(r, scale([]point{{15.4, 10.9}, {22.5, 8}, {22.5, 16.8}, {16.1, 13.4}}), true)

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	r.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{A: 255}), image.Point{})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// fill adds a polygon to the rasterizer, wound clockwise (on screen) to
// fill, or counterclockwise to cut a hole in what is filled clockwise.
func fill(r *vector.Rasterizer, ps []point, clockwise bool) {
	if (area(ps) > 0) != clockwise {
		for i, j := 0, len(ps)-1; i < j; i, j = i+1, j-1 {
			ps[i], ps[j] = ps[j], ps[i]
		}
	}
	r.MoveTo(float32(ps[0].x), float32(ps[0].y))
	for _, p := range ps[1:] {
		r.LineTo(float32(p.x), float32(p.y))
	}
	r.ClosePath()
}

// area is the signed area of a polygon, positive when clockwise on screen
// (y down).
func area(ps []point) float64 {
	a := 0.0
	for i, p := range ps {
		q := ps[(i+1)%len(ps)]
		a += p.x*q.y - q.x*p.y
	}
	return a / 2
}

func incircle(t []point) (point, float64) {
	a := math.Hypot(t[1].x-t[2].x, t[1].y-t[2].y)
	b := math.Hypot(t[0].x-t[2].x, t[0].y-t[2].y)
	c := math.Hypot(t[0].x-t[1].x, t[0].y-t[1].y)
	p := a + b + c
	center := point{(a*t[0].x + b*t[1].x + c*t[2].x) / p, (a*t[0].y + b*t[1].y + c*t[2].y) / p}
	return center, 2 * math.Abs(area(t)) / p
}

func about(ps []point, c point, k float64) []point {
	out := make([]point, len(ps))
	for i, p := range ps {
		out[i] = point{c.x + (p.x-c.x)*k, c.y + (p.y-c.y)*k}
	}
	return out
}

// thick is a line from a to b as a rectangle w wide.
func thick(a, b point, w float64) []point {
	dx, dy := b.x-a.x, b.y-a.y
	l := math.Hypot(dx, dy)
	nx, ny := -dy/l*w/2, dx/l*w/2
	return []point{{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny}, {b.x - nx, b.y - ny}, {a.x - nx, a.y - ny}}
}
