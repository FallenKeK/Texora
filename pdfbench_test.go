//go:build !windows

package main

import (
	"os"
	"testing"
	"time"
)

func TestExportPDFBigTiming(t *testing.T) {
	data, err := os.ReadFile("testdata/big.docx")
	if err != nil {
		t.Skip()
	}
	d, _ := OpenBytes("big.docx", data)
	tb := newTTFText()
	L := NewMeasurer(tb).Paginate(d, map[*Para]*ParaL{})
	t0 := time.Now()
	out, err := ExportPDFVector(d, L, tb, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d страниц за %v, %d КБ", len(L.Pages), time.Since(t0), len(out)/1024)
}
