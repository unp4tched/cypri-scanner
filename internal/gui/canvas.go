package gui

import (
	"math"
	"syscall"
	"unsafe"
)

// surface is a 32-bit top-down DIB that everything is painted into before a
// single BitBlt to the window. Double buffering removes flicker from the
// animations, and the DIB lets the same drawing code produce preview PNGs.
type surface struct {
	hdc  uintptr
	bmp  uintptr
	old  uintptr
	w    int
	h    int
	bits unsafe.Pointer
}

func newSurface(width, height int) (*surface, error) {
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, syscall.GetLastError()
	}
	defer procReleaseDC.Call(0, screen)

	dc, _, _ := procCreateCompatibleDC.Call(screen)
	if dc == 0 {
		return nil, syscall.GetLastError()
	}

	info := bitmapInfo{}
	info.Header.Size = uint32(unsafe.Sizeof(bitmapInfoHeader{}))
	info.Header.Width = int32(width)
	info.Header.Height = int32(-height) // top-down so rows read naturally
	info.Header.Planes = 1
	info.Header.BitCount = 32
	info.Header.Compression = biRgb

	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(
		screen,
		uintptr(unsafe.Pointer(&info)),
		dibRgb,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bmp == 0 || bits == nil {
		procDeleteDC.Call(dc)
		return nil, syscall.GetLastError()
	}

	old, _, _ := procSelectObject.Call(dc, bmp)
	return &surface{hdc: dc, bmp: bmp, old: old, w: width, h: height, bits: bits}, nil
}

func (s *surface) free() {
	if s.hdc == 0 {
		return
	}
	procSelectObject.Call(s.hdc, s.old)
	procDeleteObject.Call(s.bmp)
	procDeleteDC.Call(s.hdc)
	s.hdc = 0
}

func (s *surface) blitTo(destination uintptr) {
	procBitBlt.Call(destination, 0, 0, uintptr(s.w), uintptr(s.h), s.hdc, 0, 0, srcCopy)
}

// bgra returns the raw pixels (BGRA, top-down) for PNG export.
func (s *surface) bgra() []byte {
	size := s.w * s.h * 4
	return unsafe.Slice((*byte)(s.bits), size)
}

func (s *surface) fill(r rect, color uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	pen, _, _ := procCreatePen.Call(psSolid, 1, uintptr(color))
	oldBrush, _, _ := procSelectObject.Call(s.hdc, brush)
	oldPen, _, _ := procSelectObject.Call(s.hdc, pen)
	procRectangle.Call(
		s.hdc,
		uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom),
	)
	procSelectObject.Call(s.hdc, oldBrush)
	procSelectObject.Call(s.hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func (s *surface) fillRound(r rect, radius int, color uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	pen, _, _ := procCreatePen.Call(psSolid, 1, uintptr(color))
	oldBrush, _, _ := procSelectObject.Call(s.hdc, brush)
	oldPen, _, _ := procSelectObject.Call(s.hdc, pen)
	procRoundRect.Call(
		s.hdc,
		uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom),
		uintptr(radius*2), uintptr(radius*2),
	)
	procSelectObject.Call(s.hdc, oldBrush)
	procSelectObject.Call(s.hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func (s *surface) strokeRound(r rect, radius int, width int, color uint32) {
	nullBrush, _, _ := procGetStockObject.Call(5) // NULL_BRUSH
	pen, _, _ := procCreatePen.Call(psSolid, uintptr(width), uintptr(color))
	oldBrush, _, _ := procSelectObject.Call(s.hdc, nullBrush)
	oldPen, _, _ := procSelectObject.Call(s.hdc, pen)
	procRoundRect.Call(
		s.hdc,
		uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom),
		uintptr(radius*2), uintptr(radius*2),
	)
	procSelectObject.Call(s.hdc, oldBrush)
	procSelectObject.Call(s.hdc, oldPen)
	procDeleteObject.Call(pen)
}

// gradient fills a rectangle with a vertical two-stop gradient.
func (s *surface) gradient(r rect, top uint32, bottom uint32) {
	vertices := [2]triVertex{
		{X: r.Left, Y: r.Top, Red: uint16((top & 0xFF) << 8), Green: uint16(((top >> 8) & 0xFF) << 8), Blue: uint16(((top >> 16) & 0xFF) << 8), Alpha: 0},
		{X: r.Right, Y: r.Bottom, Red: uint16((bottom & 0xFF) << 8), Green: uint16(((bottom >> 8) & 0xFF) << 8), Blue: uint16(((bottom >> 16) & 0xFF) << 8), Alpha: 0},
	}
	mesh := [1]gradientRect{{UpperLeft: 0, LowerRight: 1}}
	procGradientFill.Call(
		s.hdc,
		uintptr(unsafe.Pointer(&vertices[0])),
		2,
		uintptr(unsafe.Pointer(&mesh[0])),
		1,
		gradientFillRectV,
	)
}

func (s *surface) text(value string, font uintptr, color uint32, r rect, format uint32) {
	if value == "" {
		return
	}
	oldFont, _, _ := procSelectObject.Call(s.hdc, font)
	procSetBkMode.Call(s.hdc, transparent)
	procSetTextColor.Call(s.hdc, uintptr(color))
	text := syscall.StringToUTF16(value)
	procDrawTextW.Call(
		s.hdc,
		uintptr(unsafe.Pointer(&text[0])),
		uintptr(len(text)-1),
		uintptr(unsafe.Pointer(&r)),
		uintptr(format),
	)
	procSelectObject.Call(s.hdc, oldFont)
}

func (s *surface) dot(cx, cy, radius int, color uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	pen, _, _ := procCreatePen.Call(psSolid, 1, uintptr(color))
	oldBrush, _, _ := procSelectObject.Call(s.hdc, brush)
	oldPen, _, _ := procSelectObject.Call(s.hdc, pen)
	procEllipse.Call(
		s.hdc,
		uintptr(cx-radius), uintptr(cy-radius), uintptr(cx+radius), uintptr(cy+radius),
	)
	procSelectObject.Call(s.hdc, oldBrush)
	procSelectObject.Call(s.hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func (s *surface) ring(cx, cy, radius, width int, startDeg, sweepDeg float64, color uint32) {
	if math.Abs(sweepDeg) < 0.2 {
		return
	}
	if sweepDeg > 359.6 {
		sweepDeg = 359.6 // an exact 360 confuses Arc; leave a hairline gap
	}
	box := rect{
		Left:   int32(cx - radius),
		Top:    int32(cy - radius),
		Right:  int32(cx + radius),
		Bottom: int32(cy + radius),
	}
	// GDI draws arcs counter-clockwise, so supply the end point first.
	from := startDeg + sweepDeg
	to := startDeg
	x1, y1 := pointOnCircle(cx, cy, radius, from)
	x2, y2 := pointOnCircle(cx, cy, radius, to)

	nullBrush, _, _ := procGetStockObject.Call(5)
	pen, _, _ := procCreatePen.Call(psSolid, uintptr(width), uintptr(color))
	oldBrush, _, _ := procSelectObject.Call(s.hdc, nullBrush)
	oldPen, _, _ := procSelectObject.Call(s.hdc, pen)
	procArc.Call(
		s.hdc,
		uintptr(box.Left), uintptr(box.Top), uintptr(box.Right), uintptr(box.Bottom),
		uintptr(x1), uintptr(y1), uintptr(x2), uintptr(y2),
	)
	procSelectObject.Call(s.hdc, oldBrush)
	procSelectObject.Call(s.hdc, oldPen)
	procDeleteObject.Call(pen)
}

func (s *surface) line(x1, y1, x2, y2, width int, color uint32) {
	pen, _, _ := procCreatePen.Call(psSolid, uintptr(width), uintptr(color))
	oldPen, _, _ := procSelectObject.Call(s.hdc, pen)
	procMoveToEx.Call(s.hdc, uintptr(x1), uintptr(y1), 0)
	procLineTo.Call(s.hdc, uintptr(x2), uintptr(y2))
	procSelectObject.Call(s.hdc, oldPen)
	procDeleteObject.Call(pen)
}

func (s *surface) polyline(points []point, width int, color uint32) {
	if len(points) < 2 {
		return
	}
	pen, _, _ := procCreatePen.Call(psSolid, uintptr(width), uintptr(color))
	oldPen, _, _ := procSelectObject.Call(s.hdc, pen)
	procPolyline.Call(s.hdc, uintptr(unsafe.Pointer(&points[0])), uintptr(len(points)))
	procSelectObject.Call(s.hdc, oldPen)
	procDeleteObject.Call(pen)
}

// checkMark draws a tick, revealing it progressively as progress goes 0 -> 1.
func (s *surface) checkMark(cx, cy, size int, progress float64, color uint32) {
	if progress <= 0 {
		return
	}
	first := point{X: int32(cx - size/2), Y: int32(cy)}
	elbow := point{X: int32(cx - size/10), Y: int32(cy + size/2)}
	last := point{X: int32(cx + size/2), Y: int32(cy - size/2)}

	firstLength := distance(first, elbow)
	secondLength := distance(elbow, last)
	travelled := (firstLength + secondLength) * clampFloat(progress, 0, 1)

	points := []point{first}
	if travelled <= firstLength {
		points = append(points, pointAlong(first, elbow, travelled/firstLength))
	} else {
		points = append(points, elbow)
		points = append(points, pointAlong(elbow, last, (travelled-firstLength)/secondLength))
	}
	s.polyline(points, maxInt(3, size/7), color)
}

func pointOnCircle(cx, cy, radius int, degrees float64) (uintptr, uintptr) {
	radians := degrees * math.Pi / 180
	return uintptr(float64(cx) + float64(radius)*math.Cos(radians)),
		uintptr(float64(cy) + float64(radius)*math.Sin(radians))
}

func distance(a, b point) float64 {
	dx := float64(b.X - a.X)
	dy := float64(b.Y - a.Y)
	return math.Hypot(dx, dy)
}

func pointAlong(from, to point, t float64) point {
	return point{
		X: int32(float64(from.X) + (float64(to.X)-float64(from.X))*t),
		Y: int32(float64(from.Y) + (float64(to.Y)-float64(from.Y))*t),
	}
}

func clampFloat(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// blend mixes two colours; t=0 returns background, t=1 foreground.
func blend(background, foreground uint32, t float64) uint32 {
	t = clampFloat(t, 0, 1)
	r := float64(background&0xFF)*(1-t) + float64(foreground&0xFF)*t
	g := float64((background>>8)&0xFF)*(1-t) + float64((foreground>>8)&0xFF)*t
	b := float64((background>>16)&0xFF)*(1-t) + float64((foreground>>16)&0xFF)*t
	return makeColor(uint32(r), uint32(g), uint32(b))
}
