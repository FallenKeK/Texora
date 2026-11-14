package main

// Картинки страниц: для печати и для запасного PDF «картинками», если векторный экспорт (pdfout.go) не удался.

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
)

func RenderPageImage(d *Doc, L *Layout, tb TextBackend, page int, dpi float64, prepare func(*Canvas)) *image.RGBA {
	k := dpi / 72
	g := L.Pages[page].G.Page
	w, h := int(g.W*k+0.5), int(g.H*k+0.5)
	cv := &Canvas{W: w, H: h, Pix: make([]uint32, w*h)}
	if prepare != nil {
		prepare(cv)
	}
	cv.ResetClip()
	cv.Clear(Hex(0xffffff))
	pp := &PagePainter{cv: cv, tb: tb, doc: d, k: k, imgs: map[string]*scaledImg{}}
	pp.DrawPage(L, L.Pages[page], "body", true)
	cv.sync()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, p := range cv.Pix {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = uint8(p>>16), uint8(p>>8), uint8(p), 255
	}
	if cv.Release != nil {
		cv.Release()
	}
	return img
}

// ExportPDF — запасной PDF из картинок страниц (текст в нём не ищется); render возвращает картинку страницы.
func ExportPDF(L *Layout, render func(page int) *image.RGBA, progress func(int, int)) ([]byte, error) {
	var buf bytes.Buffer
	var offsets []int
	obj := func(s string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(s)
	}
	n := len(L.Pages)
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	// 1 — каталог, 2 — список страниц, далее по 3 объекта на страницу (страница, содержимое, картинка)
	kids := ""
	for i := 0; i < n; i++ {
		kids += fmt.Sprintf("%d 0 R ", 3+i*3)
	}
	obj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	obj(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n", kids, n))
	for i := 0; i < n; i++ {
		img := render(i)
		var jb bytes.Buffer
		if err := jpeg.Encode(&jb, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, err
		}
		pid, cid, iid := 3+i*3, 4+i*3, 5+i*3
		w, h := L.Pages[i].G.Page.W, L.Pages[i].G.Page.H
		obj(fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>\nendobj\n", pid, w, h, iid, cid))
		content := fmt.Sprintf("q %.2f 0 0 %.2f 0 0 cm /Im0 Do Q", w, h)
		obj(fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", cid, len(content), content))
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n",
			iid, img.Rect.Dx(), img.Rect.Dy(), jb.Len())
		buf.Write(jb.Bytes())
		buf.WriteString("\nendstream\nendobj\n")
		if progress != nil {
			progress(i+1, n)
		}
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Producer (Texora) >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return buf.Bytes(), nil
}

// rotate90 — поворот картинки на 90° по часовой стрелке (печать альбомных страниц).
func rotate90(src *image.RGBA) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			si := y*src.Stride + x*4
			nx, ny := h-1-y, x
			di := ny*dst.Stride + nx*4
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
