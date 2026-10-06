package pdf

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/scoming-dev/tools/markdown/internal/core"
)

// muPDFTestPage builds the kind of HTML go-fitz prints for one page: absolute
// top/left per visual run, sibling <p> elements for table cells that are far
// apart, inline <b>/<span> for emphasis, and one image with its placement
// matrix.
func muPDFTestPage(t *testing.T) string {
	t.Helper()
	var buffer bytes.Buffer
	frame := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for x := 0; x < 8; x++ {
		for y := 0; y < 4; y++ {
			frame.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 60), B: 128, A: 255})
		}
	}
	if err := png.Encode(&buffer, frame); err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString(buffer.Bytes())

	return fmt.Sprintf(`<div id="page0" style="width:612.0pt;height:792.0pt">
<p style="top:56.0pt;left:72.0pt;line-height:20.0pt"><b><span style="font-family:Arial,sans-serif;font-size:20.0pt;color:#000000">Big Heading</span></b></p>
<p style="top:93.2pt;left:72.0pt;line-height:11.0pt"><b><span style="font-family:Arial,sans-serif;font-size:11.0pt;color:#000000">Bold label:</span></b><span style="font-family:Arial,sans-serif;font-size:11.0pt;color:#000000"> normal value</span></p>
<p style="top:133.2pt;left:72.0pt;line-height:11.0pt"><span style="font-family:Arial,sans-serif;font-size:11.0pt;color:#000000">Name</span></p>
<p style="top:133.2pt;left:260.0pt;line-height:11.0pt"><span style="font-family:Arial,sans-serif;font-size:11.0pt;color:#000000">Qty</span></p>
<p style="top:153.2pt;left:72.0pt;line-height:11.0pt"><span style="font-family:Arial,sans-serif;font-size:11.0pt;color:#000000">Apple</span></p>
<p style="top:153.2pt;left:260.0pt;line-height:11.0pt"><span style="font-family:Arial,sans-serif;font-size:11.0pt;color:#000000">3</span></p>
<img style="position:absolute;transform:matrix(33.333337,0,-0,33.333337,262.6667,187.33335)" src="data:image/png;base64,%s">
</div>`, payload)
}

func TestParsePDFPageHTMLReadsMuPDFOutput(t *testing.T) {
	page, err := parsePDFPageHTML(muPDFTestPage(t))
	if err != nil {
		t.Fatal(err)
	}
	if page.width != 612 || page.height != 792 {
		t.Fatalf("page box = %vx%v, want 612x792", page.width, page.height)
	}
	if len(page.rects) != 7 {
		t.Fatalf("runs = %d, want 7: %#v", len(page.rects), page.rects)
	}

	heading := page.rects[0]
	if heading.Text != "Big Heading" || heading.FontSize != 20 || !heading.bold() {
		t.Fatalf("heading run = %#v", heading)
	}
	if heading.Left != 72 || heading.Top != 792-56 || heading.Bottom != 792-76 {
		t.Fatalf("heading geometry = %#v", heading)
	}

	bold, plain := page.rects[1], page.rects[2]
	if !bold.bold() || plain.bold() {
		t.Fatalf("inline emphasis = bold:%v plain:%v", bold.bold(), plain.bold())
	}
	if math.Abs(plain.Left-bold.Right) > 1e-9 {
		t.Fatalf("inline runs are not adjacent: bold right=%v plain left=%v", bold.Right, plain.Left)
	}

	name, quantity := page.rects[3], page.rects[4]
	if name.Left != 72 || quantity.Left != 260 {
		t.Fatalf("cell positions = %v, %v", name.Left, quantity.Left)
	}
	if gap := quantity.Left - name.Right; gap <= pdfGapLimit(11) {
		t.Fatalf("estimated widths swallowed the cell gap: %v", gap)
	}

	if len(page.images) != 1 {
		t.Fatalf("images = %d, want 1", len(page.images))
	}
	placed := page.images[0]
	if !almostEqual(placed.Left, 100) || !almostEqual(placed.Right, 300) || !almostEqual(placed.TopDown, 92) {
		t.Fatalf("image placement = %#v", placed)
	}
	if placed.PixelWidth != 8 || placed.PixelHeight != 4 || placed.MIMEType != "image/png" || len(placed.Data) == 0 {
		t.Fatalf("image payload = %#v", placed)
	}
}

// TestParsePDFPageHTMLFeedsLayout checks the whole chain: MuPDF HTML to runs to
// lines to blocks to Markdown, so a change in MuPDF's output shape fails here
// rather than silently flattening tables or dropping emphasis.
func TestParsePDFPageHTMLFeedsLayout(t *testing.T) {
	page, err := parsePDFPageHTML(muPDFTestPage(t))
	if err != nil {
		t.Fatal(err)
	}
	lines := buildPDFTextLines(page.rects, page.height)
	blocks := buildPDFLineBlocks(lines, nil, core.NormalizePDFOptions(core.PDFOptions{}), core.TableFormatMarkdown)
	markdown, err := joinPDFBlocks(blocks)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Big Heading",
		"**Bold label:** normal value",
		"| Name | Qty |",
		"| Apple | 3 |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown is missing %q:\n%s", want, markdown)
		}
	}
	// lute inserts zero width spaces between adjacent emphasis runs; they must
	// not leak into the converted document.
	if strings.ContainsRune(markdown, '\u200b') {
		t.Fatalf("lute introduced a zero width space:\n%s", markdown)
	}
}

func TestParsePDFDataURIDecodesWrappedBase64(t *testing.T) {
	// MuPDF wraps the base64 payload across lines inside the attribute.
	payload := base64.StdEncoding.EncodeToString([]byte("hello world"))
	wrapped := payload[:6] + "\n" + payload[6:]
	mimeType, data, ok := parsePDFDataURI("data:image/png;base64," + wrapped)
	if !ok || mimeType != "image/png" || string(data) != "hello world" {
		t.Fatalf("decoded %q %q %v", mimeType, data, ok)
	}
	if _, _, ok := parsePDFDataURI("https://example.com/a.png"); ok {
		t.Fatal("a remote image must be rejected")
	}
}

func almostEqual(left, right float64) bool {
	return math.Abs(left-right) < 0.01
}

// pdfTestEncodedImage builds a noisy PNG so that compression has something to
// win, and returns it with its pixel size.
func pdfTestEncodedImage(t *testing.T, width, height int) ([]byte, uint, uint) {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			frame.Set(x, y, color.RGBA{
				R: uint8((x*7 + y*13) % 256),
				G: uint8((x*31 + y*17) % 256),
				B: uint8((x*3 + y*29) % 256),
				A: 255,
			})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, frame); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes(), uint(width), uint(height)
}

// TestRenderImageRegionDownscalesToTheResolutionCeiling covers the case the
// compression exists for: a big bitmap placed in a small box.
func TestRenderImageRegionDownscalesToTheResolutionCeiling(t *testing.T) {
	data, pixelWidth, pixelHeight := pdfTestEncodedImage(t, 2000, 1000)
	placed := pdfPlacedImage{
		Left: 0, Right: 200, Top: 792, Bottom: 692, // 200x100pt => 720dpi effective
		PixelWidth: pixelWidth, PixelHeight: pixelHeight,
		Data: data, MIMEType: "image/png",
	}
	options := core.NormalizePDFOptions(core.PDFOptions{RenderDPI: 200, RenderFormat: core.PDFImageFormatJPEG})
	encoded, mimeType, err := (&pdfDocument{}).RenderImageRegion(0, placed, options)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/jpeg" {
		t.Fatalf("mime type = %q, want image/jpeg", mimeType)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	// 200dpi over a 200x100pt box is 556x278 pixels.
	if config.Width > 600 || config.Height > 300 {
		t.Fatalf("image was not downscaled: %dx%d", config.Width, config.Height)
	}
	if config.Width < 500 || config.Height < 250 {
		t.Fatalf("image was downscaled too far: %dx%d", config.Width, config.Height)
	}
	if len(encoded) >= len(data) {
		t.Fatalf("compression did not shrink the image: %d >= %d bytes", len(encoded), len(data))
	}
}

// TestRenderImageRegionKeepsSmallImagesAtNativeSize makes sure the ceiling only
// ever removes pixels, never adds them.
func TestRenderImageRegionKeepsSmallImagesAtNativeSize(t *testing.T) {
	data, pixelWidth, pixelHeight := pdfTestEncodedImage(t, 400, 200)
	placed := pdfPlacedImage{
		Left: 0, Right: 400, Top: 792, Bottom: 592, // 72dpi effective
		PixelWidth: pixelWidth, PixelHeight: pixelHeight,
		Data: data, MIMEType: "image/png",
	}
	options := core.NormalizePDFOptions(core.PDFOptions{RenderDPI: 200, RenderFormat: core.PDFImageFormatJPEG})
	encoded, mimeType, err := (&pdfDocument{}).RenderImageRegion(0, placed, options)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/jpeg" {
		t.Fatalf("mime type = %q, want image/jpeg", mimeType)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 400 || config.Height != 200 {
		t.Fatalf("a small image changed size: %dx%d", config.Width, config.Height)
	}
}

// TestRenderImageRegionPassesThroughMatchingPayload keeps the fast path: PNG
// output requested and PNG payload already within the ceiling.
func TestRenderImageRegionPassesThroughMatchingPayload(t *testing.T) {
	data, pixelWidth, pixelHeight := pdfTestEncodedImage(t, 100, 50)
	placed := pdfPlacedImage{
		Left: 0, Right: 100, Top: 792, Bottom: 742,
		PixelWidth: pixelWidth, PixelHeight: pixelHeight,
		Data: data, MIMEType: "image/png",
	}
	options := core.NormalizePDFOptions(core.PDFOptions{RenderDPI: 200, RenderFormat: core.PDFImageFormatPNG})
	encoded, mimeType, err := (&pdfDocument{}).RenderImageRegion(0, placed, options)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/png" || !bytes.Equal(encoded, data) {
		t.Fatalf("matching payload should pass through untouched, got %q (%d bytes)", mimeType, len(encoded))
	}
}

// TestEncodePDFImageFlattensTransparencyOntoWhite covers stamps and masks:
// JPEG has no alpha channel, so transparency has to become white rather than
// black.
func TestEncodePDFImageFlattensTransparencyOntoWhite(t *testing.T) {
	frame := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	encoded, mimeType, err := encodePDFImage(frame, core.NormalizePDFOptions(core.PDFOptions{RenderFormat: core.PDFImageFormatJPEG}))
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/jpeg" {
		t.Fatalf("mime type = %q, want image/jpeg", mimeType)
	}
	decoded, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	red, green, blue, _ := decoded.At(0, 0).RGBA()
	if red < 0xf000 || green < 0xf000 || blue < 0xf000 {
		t.Fatalf("transparent pixels should become white, got %v %v %v", red, green, blue)
	}
}

// writePDFTestDocument builds a small valid PDF with one text line per page, so
// the page pipeline can be exercised against the real engine without checking a
// binary fixture into the repository.
func writePDFTestDocument(pages int) []byte {
	fontObject := 3 + 2*pages
	kids := make([]string, 0, pages)
	for index := 1; index <= pages; index++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+2*(index-1)))
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages),
	}
	for index := 1; index <= pages; index++ {
		content := fmt.Sprintf("BT /F1 12 Tf 72 700 Td (Page %d heading) Tj ET", index)
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontObject, 4+2*(index-1)),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		)
	}
	objects = append(objects, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	var builder strings.Builder
	builder.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, body := range objects {
		offsets[index+1] = builder.Len()
		fmt.Fprintf(&builder, "%d 0 obj\n%s\nendobj\n", index+1, body)
	}
	startxref := builder.Len()
	fmt.Fprintf(&builder, "xref\n0 %d\n", len(objects)+1)
	builder.WriteString("0000000000 65535 f \n")
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&builder, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&builder, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, startxref)
	return []byte(builder.String())
}

// writePDFImagePagesTestDocument builds a two-page PDF in the shape of a CAD
// export whose text was converted to curves: page one carries nothing but a
// small stamp-like picture and page two is covered by one picture. The engine
// reports no text for either page.
func writePDFImagePagesTestDocument(t *testing.T) []byte {
	t.Helper()
	var picture bytes.Buffer
	frame := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for x := 0; x < 8; x++ {
		for y := 0; y < 4; y++ {
			frame.Set(x, y, color.RGBA{R: uint8(20 * x), G: uint8(60 * y), B: 200, A: 255})
		}
	}
	if err := jpeg.Encode(&picture, frame, nil); err != nil {
		t.Fatal(err)
	}
	payload := picture.Bytes()

	objects := [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im0 7 0 R >> >> /Contents 4 0 R >>"),
		pdfTestContentStream("q 72 0 0 36 270 60 cm /Im0 Do Q"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im0 7 0 R >> >> /Contents 6 0 R >>"),
		pdfTestContentStream("q 612 0 0 792 0 0 cm /Im0 Do Q"),
		[]byte(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 4 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", len(payload), payload)),
	}

	var builder bytes.Buffer
	builder.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, body := range objects {
		offsets[index+1] = builder.Len()
		fmt.Fprintf(&builder, "%d 0 obj\n", index+1)
		builder.Write(body)
		builder.WriteString("\nendobj\n")
	}
	startxref := builder.Len()
	fmt.Fprintf(&builder, "xref\n0 %d\n", len(objects)+1)
	builder.WriteString("0000000000 65535 f \n")
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&builder, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&builder, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, startxref)
	return builder.Bytes()
}

func pdfTestContentStream(content string) []byte {
	return []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
}

func pdfTestNameHandler(_ context.Context, image core.Image) (string, error) {
	return image.Name, nil
}

// TestConvertPDFRasterizesDecorationOnlyPage covers the drawing-sheet case: a
// page with no text whose only picture is a stamp must become a whole-page
// picture, because exporting the stamp alone would lose the sheet. A page
// covered by its picture keeps the embedded image.
func TestConvertPDFRasterizesDecorationOnlyPage(t *testing.T) {
	data := writePDFImagePagesTestDocument(t)
	settings := &core.Settings{
		PDF:          core.NormalizePDFOptions(core.PDFOptions{}),
		ImageHandler: pdfTestNameHandler,
	}
	result, err := convertPDF(context.Background(), data, settings)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Metadata["pdf_rendered_pages"]; got != "1" {
		t.Fatalf("rendered pages = %q, want 1:\n%s", got, result.Markdown)
	}
	want := "![第 1 页](pdf-page-1.jpg)\n\n![](pdf-page-2-image-1.jpg)"
	if result.Markdown != want {
		t.Fatalf("unexpected markdown:\n%q\nwant:\n%q", result.Markdown, want)
	}
}

// TestConvertPDFKeepsStampWhenRenderingIsDisabled pins the fallback flag: a
// caller that opted out of rasterizing text-less pages still gets the stamp.
func TestConvertPDFKeepsStampWhenRenderingIsDisabled(t *testing.T) {
	data := writePDFImagePagesTestDocument(t)
	settings := &core.Settings{
		PDF:          core.NormalizePDFOptions(core.PDFOptions{DisablePageRenderFallback: true}),
		ImageHandler: pdfTestNameHandler,
	}
	result, err := convertPDF(context.Background(), data, settings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Markdown, "pdf-page-1.jpg") {
		t.Fatalf("rendering was disabled but a page was rasterized:\n%s", result.Markdown)
	}
	if !strings.Contains(result.Markdown, "pdf-page-1-image-1.jpg") {
		t.Fatalf("the stamp should have been kept:\n%s", result.Markdown)
	}
}

// TestConvertPDFPageWorkersMatchSequential is the contract for the parallel page
// pipeline: converting pages concurrently must reproduce the sequential output
// byte for byte, because pages are independent and are reassembled in order.
func TestConvertPDFPageWorkersMatchSequential(t *testing.T) {
	data := writePDFTestDocument(6)
	convert := func(workers int) *core.Result {
		settings := &core.Settings{PDF: core.NormalizePDFOptions(core.PDFOptions{
			PageConcurrency:           workers,
			DisablePageRenderFallback: true,
		})}
		result, err := convertPDF(context.Background(), data, settings)
		if err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		return result
	}
	sequential := convert(1)
	if !strings.Contains(sequential.Markdown, "Page 1 heading") || !strings.Contains(sequential.Markdown, "Page 6 heading") {
		t.Fatalf("fixture did not convert as expected:\n%s", sequential.Markdown)
	}
	for _, workers := range []int{2, 4, 8} {
		parallel := convert(workers)
		if parallel.Markdown != sequential.Markdown {
			t.Fatalf("workers=%d changed the Markdown:\n--- sequential ---\n%s\n--- parallel ---\n%s", workers, sequential.Markdown, parallel.Markdown)
		}
		if parallel.Metadata["pdf_converted_pages"] != sequential.Metadata["pdf_converted_pages"] {
			t.Fatalf("workers=%d reported %s converted pages, want %s", workers, parallel.Metadata["pdf_converted_pages"], sequential.Metadata["pdf_converted_pages"])
		}
	}
}

// TestConvertPDFPageWorkersRespectDocumentBudgets pins the fallback: the two
// document-wide image budgets are spent in page order, so a configured budget
// keeps the conversion sequential and reproducible.
func TestConvertPDFPageWorkersRespectDocumentBudgets(t *testing.T) {
	data := writePDFTestDocument(4)
	options := core.NormalizePDFOptions(core.PDFOptions{PageConcurrency: 8, MaxRenderedPages: 1, DisablePageRenderFallback: true})
	if options.PageWorkers() != 8 {
		t.Fatalf("PageWorkers() = %d, want 8", options.PageWorkers())
	}
	settings := &core.Settings{PDF: options}
	result, err := convertPDF(context.Background(), data, settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Markdown, "Page 4 heading") {
		t.Fatalf("a configured budget must not drop pages:\n%s", result.Markdown)
	}
}
