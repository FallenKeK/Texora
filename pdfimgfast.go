package main

import (
	"image"
	"math"
	"math/bits"
)

// pdfImgMaxPix — больше этого картинку храним уменьшенной: сканы в 600 dpi
// иначе съедают по сотне мегабайт на страницу, а на экране такая детальность не видна.
const pdfImgMaxPix = 16_000_000

func reduceFactor(w, h int) int {
	if w*h <= pdfImgMaxPix {
		return 1
	}
	return int(math.Ceil(math.Sqrt(float64(w) * float64(h) / pdfImgMaxPix)))
}

// bilevelImage — 1 бит на точку (0 — чёрный/рисовать, если не inv) в RGB,
// при необходимости сразу с уменьшением (усреднение даёт мягкие края букв).
func bilevelImage(data []byte, w, h int, inv, mask bool) *pdfImage {
	stride := (w + 7) / 8
	f := reduceFactor(w, h)
	nw, nh := max(1, w/f), max(1, h/f)
	im := &pdfImage{w: nw, h: nh, rgb: make([]uint8, nw*nh*3), mask: mask}
	bit := func(row []byte, x int) int {
		if x/8 >= len(row) {
			return 1
		}
		return int(row[x/8]>>(7-uint(x%8))) & 1
	}
	rowAt := func(y int) []byte {
		o := y * stride
		if o >= len(data) {
			return nil
		}
		return data[o:min(len(data), o+stride)]
	}
	if f == 1 {
		for y := 0; y < h; y++ {
			row := rowAt(y)
			o := y * w * 3
			for x := 0; x < w; x++ {
				v := uint8(255)
				if row != nil && bit(row, x) == 0 {
					v = 0
				}
				if inv {
					v = 255 - v
				}
				im.rgb[o], im.rgb[o+1], im.rgb[o+2] = v, v, v
				o += 3
			}
		}
		return im
	}
	// усреднение блоков f×f: считаем единичные биты построчно
	ones := make([]int, nw)
	n := f * f
	for oy := 0; oy < nh; oy++ {
		for i := range ones {
			ones[i] = 0
		}
		for dy := 0; dy < f; dy++ {
			row := rowAt(oy*f + dy)
			if row == nil {
				for i := range ones {
					ones[i] += f
				}
				continue
			}
			if f == 8 || f%8 == 0 {
				for ox := 0; ox < nw; ox++ {
					for k := 0; k < f/8; k++ {
						j := (ox*f)/8 + k
						if j < len(row) {
							ones[ox] += bits.OnesCount8(row[j])
						} else {
							ones[ox] += 8
						}
					}
				}
				continue
			}
			for ox := 0; ox < nw; ox++ {
				x0 := ox * f
				c := 0
				for x := x0; x < x0+f; x++ {
					c += bit(row, x)
				}
				ones[ox] += c
			}
		}
		o := oy * nw * 3
		for ox := 0; ox < nw; ox++ {
			v := uint8(ones[ox] * 255 / n)
			if inv {
				v = 255 - v
			}
			im.rgb[o], im.rgb[o+1], im.rgb[o+2] = v, v, v
			o += 3
		}
	}
	return im
}

// fastJPEG — быстрое преобразование обычных JPEG (YCbCr и серый) без вызова At на каждую точку.
func fastJPEG(img image.Image) *pdfImage {
	switch m := img.(type) {
	case *image.Gray:
		b := m.Rect
		w, h := b.Dx(), b.Dy()
		im := &pdfImage{w: w, h: h, rgb: make([]uint8, w*h*3)}
		for y := 0; y < h; y++ {
			src := m.Pix[y*m.Stride : y*m.Stride+w]
			o := y * w * 3
			for _, v := range src {
				im.rgb[o], im.rgb[o+1], im.rgb[o+2] = v, v, v
				o += 3
			}
		}
		return im
	case *image.YCbCr:
		b := m.Rect
		w, h := b.Dx(), b.Dy()
		im := &pdfImage{w: w, h: h, rgb: make([]uint8, w*h*3)}
		for y := 0; y < h; y++ {
			o := y * w * 3
			for x := 0; x < w; x++ {
				yi := m.YOffset(b.Min.X+x, b.Min.Y+y)
				ci := m.COffset(b.Min.X+x, b.Min.Y+y)
				yy := int32(m.Y[yi]) * 0x10101
				cb := int32(m.Cb[ci]) - 128
				cr := int32(m.Cr[ci]) - 128
				r := (yy + 91881*cr) >> 16
				g := (yy - 22554*cb - 46802*cr) >> 16
				bb := (yy + 116130*cb) >> 16
				im.rgb[o] = clampI8(r)
				im.rgb[o+1] = clampI8(g)
				im.rgb[o+2] = clampI8(bb)
				o += 3
			}
		}
		return im
	}
	return nil
}

func clampI8(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// shrinkBig — слишком крупную картинку заменяем уменьшенной копией.
func shrinkBig(im *pdfImage) *pdfImage {
	if im == nil {
		return nil
	}
	f := reduceFactor(im.w, im.h)
	if f <= 1 {
		return im
	}
	sm := im.downscaled(f)
	sm.mips = nil
	return sm
}

// pdfImageBytes — сколько памяти держит картинка вместе с уменьшенными копиями.
func pdfImageBytes(im *pdfImage) int {
	if im == nil {
		return 0
	}
	n := len(im.rgb) + len(im.a)
	for _, m := range im.mips {
		n += len(m.rgb) + len(m.a)
	}
	return n
}
