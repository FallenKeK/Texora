package main

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"strings"
	"testing"
)

const pptxNS = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

func relsXML(rs ...[3]string) string {
	s := `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	for _, r := range rs {
		s += `<Relationship Id="` + r[0] + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/` + r[1] + `" Target="` + r[2] + `"/>`
	}
	return s + `</Relationships>`
}

// testPptx — презентация из двух слайдов: заголовок из заполнителя, фигуры, группа, таблица, диаграмма.
func testPptx(t *testing.T) []byte {
	sp := func(id, geom, x, y, cx, cy, fill, text string) string {
		return `<p:sp><p:nvSpPr><p:cNvPr id="` + id + `" name="s"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="` + x + `" y="` + y + `"/><a:ext cx="` + cx + `" cy="` + cy + `"/></a:xfrm><a:prstGeom prst="` + geom + `"/>` + fill +
			`<a:ln w="12700"><a:solidFill><a:schemeClr val="accent2"/></a:solidFill></a:ln></p:spPr><p:txBody><a:bodyPr anchor="ctr"/><a:p><a:pPr algn="ctr"/><a:r><a:rPr sz="2000" b="1"><a:solidFill><a:schemeClr val="bg1"/></a:solidFill></a:rPr><a:t>` + text + `</a:t></a:r><a:r><a:rPr sz="2000" baseline="-25000"/><a:t>инд</a:t></a:r></a:p></p:txBody></p:sp>`
	}
	files := map[string]string{
		"[Content_Types].xml":             `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"ppt/presentation.xml":            `<?xml version="1.0"?><p:presentation ` + pptxNS + `><p:sldIdLst><p:sldId id="256" r:id="rId2"/><p:sldId id="257" r:id="rId3"/></p:sldIdLst><p:sldSz cx="9144000" cy="6858000"/></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": relsXML([3]string{"rId1", "slideMaster", "slideMasters/slideMaster1.xml"}, [3]string{"rId2", "slide", "slides/slide1.xml"}, [3]string{"rId3", "slide", "slides/slide2.xml"}),
		"ppt/slideMasters/slideMaster1.xml": `<?xml version="1.0"?><p:sldMaster ` + pptxNS + `><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="F4F4F8"/></a:solidFill></p:bgPr></p:bg><p:spTree>` +
			`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="457200" y="274638"/><a:ext cx="8229600" cy="1143000"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:p><a:r><a:t>Образец заголовка</a:t></a:r></a:p></p:txBody></p:sp>` +
			sp("3", "rect", "0", "6600000", "9144000", "258000", `<a:solidFill><a:schemeClr val="accent1"/></a:solidFill>`, "Подвал образца") +
			`</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` +
			`<p:txStyles><p:titleStyle><a:lvl1pPr><a:defRPr sz="4000"/></a:lvl1pPr></p:titleStyle><p:bodyStyle><a:lvl1pPr marL="342900" indent="-342900"><a:buChar char="•"/><a:defRPr sz="2400"/></a:lvl1pPr></p:bodyStyle></p:txStyles></p:sldMaster>`,
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": relsXML([3]string{"rId1", "theme", "../theme/theme1.xml"}, [3]string{"rId2", "slideLayout", "../slideLayouts/slideLayout1.xml"}),
		"ppt/slideLayouts/slideLayout1.xml":            `<?xml version="1.0"?><p:sldLayout ` + pptxNS + `><p:cSld><p:spTree/></p:cSld></p:sldLayout>`,
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": relsXML([3]string{"rId1", "slideMaster", "../slideMasters/slideMaster1.xml"}),
		"ppt/theme/theme1.xml": `<?xml version="1.0"?><a:theme ` + pptxNS + `><a:themeElements><a:clrScheme name="t"><a:dk1><a:srgbClr val="111111"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="1F2A44"/></a:dk2><a:lt2><a:srgbClr val="EEEEEE"/></a:lt2>` +
			`<a:accent1><a:srgbClr val="7C5CFF"/></a:accent1><a:accent2><a:srgbClr val="2F5D8C"/></a:accent2><a:accent3><a:srgbClr val="A32020"/></a:accent3><a:accent4><a:srgbClr val="70AD47"/></a:accent4><a:accent5><a:srgbClr val="FFC000"/></a:accent5><a:accent6><a:srgbClr val="5B9BD5"/></a:accent6></a:clrScheme>` +
			`<a:fontScheme name="f"><a:majorFont><a:latin typeface="Times New Roman"/></a:majorFont><a:minorFont><a:latin typeface="Times New Roman"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`,
		"ppt/slides/slide1.xml": `<?xml version="1.0"?><p:sld ` + pptxNS + `><p:cSld><p:spTree>` +
			`<p:sp><p:nvSpPr><p:cNvPr id="2" name="T"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:p><a:r><a:t>Заголовок первого слайда</a:t></a:r></a:p></p:txBody></p:sp>` +
			sp("3", "roundRect", "457200", "1600000", "3000000", "1200000", `<a:solidFill><a:schemeClr val="accent1"><a:lumMod val="75000"/></a:schemeClr></a:solidFill>`, "Скруглённая") +
			sp("4", "ellipse", "4000000", "1600000", "2000000", "1200000", `<a:solidFill><a:srgbClr val="2F5D8C"/></a:solidFill>`, "Эллипс") +
			`<p:grpSp><p:nvGrpSpPr><p:cNvPr id="5" name="g"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="457200" y="3200000"/><a:ext cx="4000000" cy="1000000"/><a:chOff x="0" y="0"/><a:chExt cx="8000000" cy="2000000"/></a:xfrm></p:grpSpPr>` +
			sp("6", "rect", "0", "0", "8000000", "2000000", `<a:solidFill><a:schemeClr val="tx2"/></a:solidFill>`, "В группе") + `</p:grpSp>` +
			`<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="7" name="c"/><p:cNvCxnSpPr/><p:nvPr/></p:nvCxnSpPr><p:spPr><a:xfrm flipV="1"><a:off x="5000000" y="3200000"/><a:ext cx="3000000" cy="1000000"/></a:xfrm><a:prstGeom prst="straightConnector1"/><a:ln w="25400"><a:solidFill><a:srgbClr val="A32020"/></a:solidFill><a:tailEnd type="triangle"/></a:ln></p:spPr></p:cxnSp>` +
			`<p:sp><p:nvSpPr><p:cNvPr id="8" name="B"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="457200" y="4400000"/><a:ext cx="8229600" cy="2000000"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:p><a:r><a:t>Первый пункт списка, достаточно длинный, чтобы перенестись на следующую строку внутри рамки</a:t></a:r></a:p><a:p><a:r><a:t>Второй пункт</a:t></a:r></a:p></p:txBody></p:sp>` +
			`<p:sp><p:nvSpPr><p:cNvPr id="9" name="beam"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm flipH="1"><a:off x="6000000" y="1500000"/><a:ext cx="2000000" cy="1500000"/></a:xfrm><a:custGeom><a:pathLst><a:path w="200" h="150"><a:moveTo><a:pt x="0" y="0"/></a:moveTo><a:lnTo><a:pt x="80" y="150"/></a:lnTo><a:quadBezTo><a:pt x="140" y="100"/><a:pt x="200" y="150"/></a:quadBezTo><a:arcTo wR="20" hR="20" stAng="0" swAng="5400000"/><a:close/></a:path></a:pathLst></a:custGeom><a:solidFill><a:srgbClr val="2F5D8C"><a:alpha val="65000"/></a:srgbClr></a:solidFill><a:ln w="12700"><a:solidFill><a:srgbClr val="FFFFFF"><a:alpha val="0"/></a:srgbClr></a:solidFill></a:ln></p:spPr></p:sp>` +
			`<p:sp><p:nvSpPr><p:cNvPr id="10" name="flat"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="100000" y="100000"/><a:ext cx="3000000" cy="0"/></a:xfrm><a:custGeom><a:pathLst><a:path w="300" h="0" fill="none"><a:moveTo><a:pt x="0" y="0"/></a:moveTo><a:lnTo><a:pt x="300" y="0"/></a:lnTo></a:path></a:pathLst></a:custGeom><a:ln w="12700"><a:solidFill><a:srgbClr val="A32020"/></a:solidFill></a:ln></p:spPr></p:sp>` +
			`</p:spTree></p:cSld></p:sld>`,
		"ppt/slides/_rels/slide1.xml.rels": relsXML([3]string{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"}),
		"ppt/slides/slide2.xml": `<?xml version="1.0"?><p:sld ` + pptxNS + `><p:cSld><p:spTree>` +
			`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="2" name="tbl"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="457200" y="457200"/><a:ext cx="4000000" cy="800000"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl><a:tblGrid><a:gridCol w="2000000"/><a:gridCol w="2000000"/></a:tblGrid>` +
			`<a:tr h="400000"><a:tc><a:txBody><a:bodyPr/><a:p><a:r><a:t>Параметр</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="DDDDEE"/></a:solidFill></a:tcPr></a:tc><a:tc><a:txBody><a:bodyPr/><a:p><a:r><a:t>Значение</a:t></a:r></a:p></a:txBody><a:tcPr/></a:tc></a:tr>` +
			`<a:tr h="400000"><a:tc gridSpan="2"><a:txBody><a:bodyPr/><a:p><a:r><a:t>Объединённая ячейка</a:t></a:r></a:p></a:txBody><a:tcPr/></a:tc><a:tc hMerge="1"><a:txBody><a:bodyPr/><a:p/></a:txBody><a:tcPr/></a:tc></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>` +
			`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="3" name="ch"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="457200" y="1600000"/><a:ext cx="8000000" cy="4500000"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" r:id="rId2"/></a:graphicData></a:graphic></p:graphicFrame>` +
			`</p:spTree></p:cSld></p:sld>`,
		"ppt/slides/_rels/slide2.xml.rels": relsXML([3]string{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"}, [3]string{"rId2", "chart", "../charts/chart1.xml"}),
		"ppt/charts/chart1.xml": `<?xml version="1.0"?><c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><c:chart><c:title><c:tx><c:rich><a:p><a:r><a:t>Выручка по кварталам</a:t></a:r></a:p></c:rich></c:tx></c:title><c:plotArea><c:barChart><c:barDir val="col"/>` +
			`<c:ser><c:tx><c:strRef><c:strCache><c:ptCount val="1"/><c:pt idx="0"><c:v>План</c:v></c:pt></c:strCache></c:strRef></c:tx><c:cat><c:strRef><c:strCache><c:ptCount val="2"/><c:pt idx="0"><c:v>Первый</c:v></c:pt><c:pt idx="1"><c:v>Второй</c:v></c:pt></c:strCache></c:strRef></c:cat><c:val><c:numRef><c:numCache><c:ptCount val="2"/><c:pt idx="0"><c:v>12.5</c:v></c:pt><c:pt idx="1"><c:v>20</c:v></c:pt></c:numCache></c:numRef></c:val></c:ser>` +
			`<c:ser><c:tx><c:v>Факт</c:v></c:tx><c:cat><c:strRef><c:strCache><c:ptCount val="2"/><c:pt idx="0"><c:v>Первый</c:v></c:pt><c:pt idx="1"><c:v>Второй</c:v></c:pt></c:strCache></c:strRef></c:cat><c:val><c:numRef><c:numCache><c:ptCount val="2"/><c:pt idx="0"><c:v>10</c:v></c:pt><c:pt idx="1"><c:v>-3</c:v></c:pt></c:numCache></c:numRef></c:val></c:ser>` +
			`</c:barChart><c:catAx><c:axPos val="b"/><c:title><c:tx><c:rich><a:p><a:r><a:t>Квартал</a:t></a:r></a:p></c:rich></c:tx></c:title></c:catAx><c:valAx><c:axPos val="l"/><c:majorGridlines/><c:majorUnit val="5"/></c:valAx></c:plotArea><c:legend><c:legendPos val="b"/></c:legend></c:chart></c:chartSpace>`,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPptxToPDF(t *testing.T) {
	out, err := PptxToPDF(testPptx(t))
	if err != nil {
		t.Fatal(err)
	}
	d, err := LoadPDF(out)
	if err != nil {
		t.Fatalf("PDF не читается: %v", err)
	}
	if len(d.Pages) != 2 {
		t.Fatalf("слайдов %d, ждали 2", len(d.Pages))
	}
	text := func(i int) string {
		_, gl := d.RenderPage(i, 0.5, false)
		s, _ := PageText(gl)
		return strings.Join(strings.Fields(s), " ")
	}
	t1, t2 := text(0), text(1)
	for _, want := range []string{"Заголовок первого слайда", "Скруглённая", "Эллипс", "В группе", "Подвал образца", "Второй пункт", "•"} {
		if !strings.Contains(t1, want) {
			t.Errorf("на первом слайде нет %q: %q", want, t1)
		}
	}
	if strings.Contains(t1, "Образец заголовка") {
		t.Errorf("текст заполнителя образца попал на слайд: %q", t1)
	}
	for _, want := range []string{"Параметр", "Значение", "Объединённая ячейка", "Выручка по кварталам", "Первый", "Второй", "План", "Факт", "Квартал", "20", "-5"} {
		if !strings.Contains(t2, want) {
			t.Errorf("на втором слайде нет %q: %q", want, t2)
		}
	}
	// размер слайда 10×7,5 дюйма
	if b := d.Pages[0].Box; b[2]-b[0] < 719 || b[2]-b[0] > 721 || b[3]-b[1] < 539 || b[3]-b[1] > 541 {
		t.Errorf("размер слайда %v", b)
	}
	if !bytes.Contains(out, []byte("/ca 0.65")) {
		t.Errorf("нет полупрозрачной заливки")
	}
	for _, cs := range pdfStreams(out) {
		if bytes.Contains(cs, []byte(" Q\n")) && regexp.MustCompile(`(^|\s)(NaN|[+-]?Inf)\s`).Match(cs) {
			t.Errorf("в PDF попали NaN/Inf")
		}
	}
	if !strings.Contains(string(out), "/Title") {
		t.Errorf("нет названия документа")
	}
}

func TestPptxBroken(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("PK\x03\x04мусор"), testPptxMissing(t)} {
		if _, err := PptxToPDF(data); err == nil {
			t.Errorf("испорченная презентация открылась без ошибки")
		}
	}
	// обрезанный архив не роняет программу
	full := testPptx(t)
	for _, n := range []int{len(full) / 3, len(full) / 2, len(full) - 10} {
		PptxToPDF(full[:n])
	}
}

func testPptxMissing(t *testing.T) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("ppt/presentation.xml")
	w.Write([]byte(`<p:presentation ` + pptxNS + `><p:sldIdLst/></p:presentation>`))
	zw.Close()
	return buf.Bytes()
}

func TestApplyColorMods(t *testing.T) {
	n, _ := ParseXML([]byte(`<a:schemeClr xmlns:a="a" val="accent1"><a:lumMod val="50000"/></a:schemeClr>`))
	c := applyColorMods(Hex(0x808080), n.Root())
	if r := uint32(c) >> 16 & 255; r < 0x3e || r > 0x42 {
		t.Errorf("lumMod 50%% от серого: %06x", uint32(c)&0xffffff)
	}
	n, _ = ParseXML([]byte(`<a:x xmlns:a="a"><a:tint val="0"/></a:x>`))
	if c := applyColorMods(Hex(0x204060), n.Root()); uint32(c)&0xffffff != 0xffffff {
		t.Errorf("tint 0 должен дать белый: %06x", uint32(c)&0xffffff)
	}
}

// pdfStreams — распакованные потоки PDF.
func pdfStreams(pdf []byte) [][]byte {
	var out [][]byte
	for _, m := range regexp.MustCompile(`(?s)stream\r?\n(.*?)endstream`).FindAllSubmatch(pdf, -1) {
		r, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			out = append(out, m[1])
			continue
		}
		b, _ := io.ReadAll(r)
		out = append(out, b)
	}
	return out
}
