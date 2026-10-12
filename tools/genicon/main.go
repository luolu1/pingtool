// Command genicon renders the PingTool application icon as a multi size .ico
// file. It is a build helper and is not part of the shipped program.
//
//	go run ./tools/genicon -o app.ico
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
)

type rgba struct{ r, g, b, a uint8 }

var (
	bg1         = rgba{0x16, 0x24, 0x38, 0xff}
	bg2         = rgba{0x1f, 0x39, 0x57, 0xff}
	green       = rgba{0x2e, 0xcc, 0x71, 0xff}
	yellow      = rgba{0xf1, 0xc4, 0x0f, 0xff}
	red         = rgba{0xe7, 0x4c, 0x3c, 0xff}
	transparent = rgba{0, 0, 0, 0}
)

// bars describes the latency bars as (x, height, color) in 0..1 space.
var bars = []struct {
	x, w, h float64
	c       rgba
}{
	{0.16, 0.11, 0.30, green},
	{0.31, 0.11, 0.52, green},
	{0.46, 0.11, 0.74, green},
	{0.61, 0.11, 0.44, yellow},
	{0.76, 0.11, 0.22, red},
}

func mix(a, b rgba, t float64) rgba {
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return rgba{m(a.r, b.r), m(a.g, b.g), m(a.b, b.b), 0xff}
}

func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	f := float64(size)

	// vertical gradient background
	for y := 0; y < size; y++ {
		c := mix(bg2, bg1, float64(y)/f)
		for x := 0; x < size; x++ {
			img.Set(x, y, color.NRGBA{c.r, c.g, c.b, c.a})
		}
	}

	// rounded corners: cut the outside of a circle of radius r
	r := f * 0.22
	round := func(x, y int) bool {
		fx, fy := float64(x)+0.5, float64(y)+0.5
		cx := clamp(fx, r, f-r)
		cy := clamp(fy, r, f-r)
		dx, dy := fx-cx, fy-cy
		return dx*dx+dy*dy > r*r
	}

	// baseline
	baseY := f * 0.84
	if baseY > f-2 {
		baseY = f - 2
	}
	baseline := image.Rect(int(f*0.12), int(baseY)-max(1, int(f*0.02)), int(f*0.88), int(baseY))
	tmp := image.NewNRGBA(img.Bounds())
	draw.Draw(tmp, baseline, &image.Uniform{color.NRGBA{0x3a, 0x55, 0x74, 0xff}}, image.Point{}, draw.Src)
	draw.Draw(img, baseline, tmp, baseline.Min, draw.Src)

	for _, b := range bars {
		x0 := int(b.x * f)
		x1 := int((b.x + b.w) * f)
		y0 := int(baseY - b.h*f)
		y1 := int(baseY)
		if x1 <= x0 {
			x1 = x0 + 1
		}
		rect := image.Rect(x0, y0, x1, y1)
		draw.Draw(img, rect, &image.Uniform{color.NRGBA{b.c.r, b.c.g, b.c.b, b.c.a}}, image.Point{}, draw.Src)
	}

	// apply rounded corners
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if round(x, y) {
				img.Set(x, y, color.NRGBA{0, 0, 0, 0})
			}
		}
	}
	return img
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	out := flag.String("o", "app.ico", "output .ico file")
	flag.Parse()

	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	images := make([][]byte, len(sizes))
	for i, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, render(s)); err != nil {
			log.Fatalf("encode %d: %v", s, err)
		}
		images[i] = buf.Bytes()
	}

	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, uint16(0))
	binary.Write(&ico, binary.LittleEndian, uint16(1))
	binary.Write(&ico, binary.LittleEndian, uint16(len(sizes)))

	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		b := byte(s)
		if s >= 256 {
			b = 0
		}
		ico.WriteByte(b)                                    // width
		ico.WriteByte(b)                                    // height
		ico.WriteByte(0)                                    // palette
		ico.WriteByte(0)                                    // reserved
		binary.Write(&ico, binary.LittleEndian, uint16(1))  // planes
		binary.Write(&ico, binary.LittleEndian, uint16(32)) // bpp
		binary.Write(&ico, binary.LittleEndian, uint32(len(images[i])))
		binary.Write(&ico, binary.LittleEndian, uint32(offset))
		offset += len(images[i])
	}
	for _, img := range images {
		ico.Write(img)
	}

	if err := os.WriteFile(*out, ico.Bytes(), 0644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s (%d bytes, %d sizes)\n", *out, ico.Len(), len(sizes))
}

var _ = transparent
