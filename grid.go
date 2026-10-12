//go:build windows

package main

import (
	"time"

	"github.com/lxn/walk"

	"pingtool/internal/pinger"
)

// Package level colors shared by the grid and the table cell styler.
var (
	colorOK     = walk.RGB(0x2e, 0xcc, 0x71)
	colorSlow   = walk.RGB(0xf1, 0xc4, 0x0f)
	colorFail   = walk.RGB(0xe7, 0x4c, 0x3c)
	colorEmpty  = walk.RGB(0xdc, 0xdf, 0xe4)
	colorCanvas = walk.RGB(0xfa, 0xfb, 0xfd)
	colorCurve  = walk.RGB(0x2f, 0x6f, 0xb5)
	colorGrid   = walk.RGB(0xe6, 0xea, 0xf0)

	textOK    = walk.RGB(0x1e, 0x8e, 0x3e)
	textSlow  = walk.RGB(0xc0, 0x6a, 0x00)
	textFail  = walk.RGB(0xc0, 0x39, 0x2b)
	textPlain = walk.RGB(0x33, 0x33, 0x33)
)

// gridRenderer owns the brushes and pens used to paint the status grid and the
// latency curve.
type gridRenderer struct {
	ok    walk.Brush
	slow  walk.Brush
	fail  walk.Brush
	empty walk.Brush
	bg    walk.Brush

	curvePen walk.Pen
	slowPen  walk.Pen
	gridPen  walk.Pen
}

func newGridRenderer() *gridRenderer {
	mk := func(c walk.Color) walk.Brush {
		br, err := walk.NewSolidColorBrush(c)
		if err != nil {
			return nil
		}
		return br
	}
	mkPen := func(c walk.Color) walk.Pen {
		p, err := walk.NewCosmeticPen(walk.PenSolid, c)
		if err != nil {
			return nil
		}
		return p
	}
	return &gridRenderer{
		ok:    mk(colorOK),
		slow:  mk(colorSlow),
		fail:  mk(colorFail),
		empty: mk(colorEmpty),
		bg:    mk(colorCanvas),

		curvePen: mkPen(colorCurve),
		slowPen:  mkPen(colorSlow),
		gridPen:  mkPen(colorGrid),
	}
}

// widgetRect returns the client rectangle of w in pixels, with the origin at
// (0, 0). Canvas.BoundsPixels() must not be used: for a window DC it reports the
// device (screen) extent instead of the widget size, which would make content
// that is anchored to the bottom or right edge disappear.
func widgetRect(w *walk.CustomWidget, dirty walk.Rectangle) walk.Rectangle {
	if w != nil {
		if b := w.BoundsPixels(); b.Width > 0 && b.Height > 0 {
			return walk.Rectangle{Width: b.Width, Height: b.Height}
		}
	}
	dirty.X, dirty.Y = 0, 0
	return dirty
}

// paintGrid draws one small square per probe. Green means the probe succeeded,
// yellow means it succeeded but exceeded the high latency threshold and red
// means the probe failed.
func (a *App) paintGrid(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	if a.gridR == nil {
		return nil
	}
	bounds := widgetRect(a.grid, updateBounds)
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return nil
	}
	if err := canvas.FillRectanglePixels(a.gridR.bg, bounds); err != nil {
		return err
	}

	const (
		cell = 14
		gap  = 2
	)
	step := cell + gap

	cols := bounds.Width / step
	if cols < 1 {
		cols = 1
	}
	rows := bounds.Height / step
	if rows < 1 {
		rows = 1
	}
	capacity := cols * rows

	probes := a.gridProbes
	total := a.gridTotal
	if total <= 0 {
		total = len(probes)
	}
	show := total
	if show > capacity {
		show = capacity
	}
	offset := total - show
	if offset < 0 {
		offset = 0
	}

	for i := 0; i < show; i++ {
		idx := offset + i
		col := i % cols
		row := i / cols
		rect := walk.Rectangle{
			X:      bounds.X + col*step,
			Y:      bounds.Y + row*step,
			Width:  cell,
			Height: cell,
		}
		br := a.gridR.empty
		if idx < len(probes) {
			br = a.gridR.brushFor(probes[idx], a.threshold)
		}
		if err := canvas.FillRectanglePixels(br, rect); err != nil {
			return err
		}
	}
	return nil
}

func (g *gridRenderer) brushFor(p pinger.Probe, threshold time.Duration) walk.Brush {
	switch {
	case !p.OK:
		return g.fail
	case threshold > 0 && p.RTT > threshold:
		return g.slow
	default:
		return g.ok
	}
}

// paintChart draws the latency as a line chart (折线图). Successful probes are
// joined by a polyline whose segment turns orange when either end exceeds the
// high latency threshold; each probe gets a small marker coloured green /
// orange / red. Failed probes sit on the baseline so loss shows as a gap plus a
// red mark. A dashed-free threshold line and faint gridlines give a scale.
func (a *App) paintChart(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	if a.gridR == nil {
		return nil
	}
	bounds := widgetRect(a.chart, updateBounds)
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return nil
	}
	if err := canvas.FillRectanglePixels(a.gridR.bg, bounds); err != nil {
		return err
	}

	probes := a.gridProbes
	if len(probes) == 0 {
		return nil
	}
	series := downsample(probes, 1000, a.threshold)
	if len(series) == 0 {
		return nil
	}

	const pad = 6
	plotTop := bounds.Y + pad
	plotH := bounds.Height - pad*2
	if plotH < 1 {
		plotTop = bounds.Y
		plotH = bounds.Height
	}

	var maxRTT time.Duration
	for _, p := range series {
		if p.OK && p.RTT > maxRTT {
			maxRTT = p.RTT
		}
	}
	scale := maxRTT
	if scale <= 0 {
		scale = time.Millisecond
	}
	scale += scale / 5 // headroom so the peak does not touch the top edge

	// faint horizontal gridlines
	if a.gridR.gridPen != nil {
		for i := 0; i <= 4; i++ {
			y := plotTop + plotH*i/4
			if err := canvas.DrawLinePixels(a.gridR.gridPen,
				walk.Point{X: bounds.X, Y: y},
				walk.Point{X: bounds.X + bounds.Width, Y: y}); err != nil {
				return err
			}
		}
	}

	// high latency threshold line
	if a.threshold > 0 && a.threshold < scale && a.gridR.slowPen != nil {
		y := plotTop + plotH - int(float64(a.threshold)/float64(scale)*float64(plotH))
		if y < plotTop {
			y = plotTop
		}
		if y > plotTop+plotH {
			y = plotTop + plotH
		}
		if err := canvas.DrawLinePixels(a.gridR.slowPen,
			walk.Point{X: bounds.X, Y: y},
			walk.Point{X: bounds.X + bounds.Width, Y: y}); err != nil {
			return err
		}
	}

	n := len(series)
	xAt := func(i int) int {
		if n <= 1 {
			return bounds.X + bounds.Width/2
		}
		return bounds.X + i*(bounds.Width-1)/(n-1)
	}
	yAt := func(d time.Duration) int {
		y := plotTop + plotH - int(float64(d)/float64(scale)*float64(plotH))
		if y < plotTop {
			y = plotTop
		}
		if y > plotTop+plotH {
			y = plotTop + plotH
		}
		return y
	}

	// polyline through consecutive successful probes
	if a.gridR.curvePen != nil {
		for i := 0; i+1 < n; i++ {
			p0, p1 := series[i], series[i+1]
			if !p0.OK || !p1.OK {
				continue
			}
			pen := a.gridR.curvePen
			if a.threshold > 0 && (p0.RTT > a.threshold || p1.RTT > a.threshold) {
				pen = a.gridR.slowPen
			}
			if err := canvas.DrawLinePixels(pen,
				walk.Point{X: xAt(i), Y: yAt(p0.RTT)},
				walk.Point{X: xAt(i + 1), Y: yAt(p1.RTT)}); err != nil {
				return err
			}
		}
	}

	// vertex markers
	const dot = 3
	for i, p := range series {
		x := xAt(i)
		y := plotTop + plotH - dot // failures rest on the baseline
		if p.OK {
			y = yAt(p.RTT)
		}
		rect := walk.Rectangle{X: x - dot, Y: y - dot, Width: dot * 2, Height: dot * 2}
		if err := canvas.FillRectanglePixels(a.gridR.brushFor(p, a.threshold), rect); err != nil {
			return err
		}
	}
	return nil
}

// downsample reduces the probe series to at most max buckets so the curve stays
// cheap to draw. Each bucket keeps its most severe probe (fail > slow > ok), so
// packet loss is never hidden by the reduction.
func downsample(probes []pinger.Probe, max int, threshold time.Duration) []pinger.Probe {
	n := len(probes)
	if max < 1 || n <= max {
		return probes
	}
	out := make([]pinger.Probe, 0, max)
	for b := 0; b < max; b++ {
		lo := b * n / max
		hi := (b + 1) * n / max
		if hi <= lo {
			hi = lo + 1
		}
		worst := probes[lo]
		worstRank := probeRank(worst, threshold)
		for _, p := range probes[lo+1 : hi] {
			if r := probeRank(p, threshold); r > worstRank {
				worst, worstRank = p, r
			}
		}
		out = append(out, worst)
	}
	return out
}

func probeRank(p pinger.Probe, threshold time.Duration) int {
	switch {
	case !p.OK:
		return 2
	case threshold > 0 && p.RTT > threshold:
		return 1
	default:
		return 0
	}
}
