package pdf

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/scoming-dev/tools/markdown/internal/core"
)

func TestPDFLayoutReflowsWrappedLinesIntoOneParagraph(t *testing.T) {
	html := pdfTestPage(
		pdfTestLine(131.2, 72, 11, 11, "This is a single paragraph that is long enough to wrap across"),
		pdfTestLine(149.2, 72, 11, 11, "multiple visual lines. Every one of them stays in the same"),
		pdfTestLine(167.2, 72, 11, 11, "paragraph."),
		pdfTestLine(357.2, 72, 11, 11, "And this is a second paragraph that must remain separate."),
	)
	want := "This is a single paragraph that is long enough to wrap across multiple visual lines. " +
		"Every one of them stays in the same paragraph.\n\n" +
		"And this is a second paragraph that must remain separate."
	if got := pdfTestMarkdown(t, html, core.PDFOptions{}, core.TableFormatMarkdown); got != want {
		t.Fatalf("unexpected reflow:\n%q\nwant:\n%q", got, want)
	}
}

func TestPDFLayoutKeepsParagraphsApartWithoutBlankLineGap(t *testing.T) {
	html := pdfTestPage(
		pdfTestLine(131.2, 72, 11, 11, "第一段第一行内容"),
		pdfTestLine(149.2, 72, 11, 11, "第一段第二行内容"),
		pdfTestLine(203.2, 72, 11, 11, "第二段内容"),
	)
	want := "第一段第一行内容第一段第二行内容\n\n第二段内容"
	if got := pdfTestMarkdown(t, html, core.PDFOptions{}, core.TableFormatMarkdown); got != want {
		t.Fatalf("unexpected CJK reflow:\n%q\nwant:\n%q", got, want)
	}
}

func TestPDFLayoutJoinsLatinWithASingleSpace(t *testing.T) {
	html := pdfTestPage(
		pdfTestLine(131.2, 72, 11, 11, "the quick brown fox jumps over the lazy dog and keeps"),
		pdfTestLine(149.2, 72, 11, 11, "running until the paragraph is complete."),
	)
	want := "the quick brown fox jumps over the lazy dog and keeps running until the paragraph is complete."
	if got := pdfTestMarkdown(t, html, core.PDFOptions{}, core.TableFormatMarkdown); got != want {
		t.Fatalf("unexpected join:\n%q\nwant:\n%q", got, want)
	}
}

func TestPDFLayoutDetectsHeadings(t *testing.T) {
	html := pdfTestPage(
		pdfTestLine(90, 72, 20, 20, "Heading One"),
		pdfTestLine(131.2, 72, 11, 11, "This paragraph is long enough that the body font size wins"),
		pdfTestLine(149.2, 72, 11, 11, "the weighted vote used for heading detection."),
		pdfTestLine(190, 72, 15, 15, "Sub Heading"),
		pdfTestLine(230.2, 72, 11, 11, "Another body paragraph with plenty of ordinary text."),
	)
	markdown := pdfTestMarkdown(t, html, core.PDFOptions{}, core.TableFormatMarkdown)
	if !strings.HasPrefix(markdown, "# Heading One\n\n") {
		t.Fatalf("first heading was not detected: %q", markdown)
	}
	if !strings.Contains(markdown, "\n\n## Sub Heading\n\n") {
		t.Fatalf("sub heading was not detected: %q", markdown)
	}
	disabled := pdfTestMarkdown(t, html, core.PDFOptions{DisableHeadingDetection: true}, core.TableFormatMarkdown)
	if strings.Contains(disabled, "#") {
		t.Fatalf("heading detection should be disabled: %q", disabled)
	}
}

func TestPDFLayoutReconstructsATable(t *testing.T) {
	html := pdfTestPage(
		pdfTestLine(143.2, 80, 11, 11, "Name"),
		pdfTestLine(143.2, 230, 11, 11, "Qty"),
		pdfTestLine(143.2, 380, 11, 11, "Price"),
		pdfTestLine(263.2, 80, 11, 11, "Apple"),
		pdfTestLine(263.2, 230, 11, 11, "3"),
		pdfTestLine(263.2, 380, 11, 11, "1.20"),
		pdfTestLine(383.2, 80, 11, 11, "Banana"),
		pdfTestLine(383.2, 230, 11, 11, "7"),
		pdfTestLine(383.2, 380, 11, 11, "0.40"),
	)
	markdown := pdfTestMarkdown(t, html, core.PDFOptions{}, core.TableFormatMarkdown)
	want := "| Name | Qty | Price |\n| --- | --- | --- |\n| Apple | 3 | 1.20 |\n| Banana | 7 | 0.40 |"
	if markdown != want {
		t.Fatalf("unexpected table:\n%q\nwant:\n%q", markdown, want)
	}
	htmlTable := pdfTestMarkdown(t, html, core.PDFOptions{}, core.TableFormatHTML)
	if !strings.Contains(htmlTable, "<table>") || !strings.Contains(htmlTable, "<td>Apple</td>") {
		t.Fatalf("HTML table format was not honoured: %q", htmlTable)
	}
	disabled := pdfTestMarkdown(t, html, core.PDFOptions{DisableTableReconstruction: true}, core.TableFormatMarkdown)
	if strings.Contains(disabled, "|") {
		t.Fatalf("table reconstruction should be disabled: %q", disabled)
	}
}

func TestPDFLayoutKeepsTableWithoutImageCellsAsATable(t *testing.T) {
	// Signature blocks put a picture in some columns, so those rows arrive with
	// fewer text cells than the header. The grid must survive.
	lines := pdfTestPage(
		pdfTestLine(166.4, 77.8, 9.9, 9.9, "序号"),
		pdfTestLine(166.4, 156.1, 9.9, 9.9, "专业名称"),
		pdfTestLine(166.4, 265.2, 9.9, 9.9, "设计"),
		pdfTestLine(166.4, 339.8, 9.9, 9.9, "校对"),
		pdfTestLine(166.4, 416.8, 9.9, 9.9, "审核"),
		pdfTestLine(166.4, 493.7, 9.9, 9.9, "审定"),
		pdfTestLine(195.4, 87.4, 9.9, 9.9, "1"),
		pdfTestLine(195.4, 163.0, 9.9, 9.9, "集输工艺"),
		pdfTestLine(224.3, 87.4, 9.9, 9.9, "2"),
		pdfTestLine(224.3, 163.0, 9.9, 9.9, "线路工程"),
	)
	images := []pdfPlacedImage{
		pdfTestPlacedImage(195.4, 280, 40, 10),
		pdfTestPlacedImage(224.3, 280, 40, 10),
	}
	markdown, stats, err := renderPDFItems(
		context.Background(),
		pdfTestImageRenderer,
		0,
		buildPDFTextLines(lines, pdfTestPageHeight),
		images,
		core.NormalizePDFOptions(core.PDFOptions{}),
		core.TableFormatMarkdown,
		func(_ context.Context, image core.Image) (string, error) { return image.Name, nil },
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats.imageCount != 2 {
		t.Fatalf("expected both images: %#v", stats)
	}
	want := "| 序号 | 专业名称 | 设计 | 校对 | 审核 | 审定 |\n" +
		"| --- | --- | --- | --- | --- | --- |\n" +
		"| 1 | 集输工艺 |  |  |  |  |\n" +
		"| 2 | 线路工程 |  |  |  |  |\n\n" +
		"![](pdf-page-1-image-1.png)\n\n![](pdf-page-1-image-2.png)"
	if markdown != want {
		t.Fatalf("table was torn apart by its image cells:\n%q\nwant:\n%q", markdown, want)
	}
}

func TestPDFLayoutRejectsShortRowsWithoutImages(t *testing.T) {
	// A row that drops a column and carries no picture must not be treated as a
	// table row; invoice line items look like this.
	html := pdfTestPage(
		pdfTestLine(143.2, 80, 11, 11, "Name"),
		pdfTestLine(143.2, 230, 11, 11, "Qty"),
		pdfTestLine(143.2, 380, 11, 11, "Price"),
		pdfTestLine(263.2, 80, 11, 11, "Apple"),
		pdfTestLine(263.2, 230, 11, 11, "3"),
	)
	markdown := pdfTestMarkdown(t, html, core.PDFOptions{}, core.TableFormatMarkdown)
	if strings.Contains(markdown, "|") {
		t.Fatalf("a short row without pictures must not be a table row: %q", markdown)
	}
	for _, want := range []string{"Name", "Qty", "Price", "Apple", "3"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("lost %q from %q", want, markdown)
		}
	}
}

func TestPDFLayoutDoesNotMistakeTwoColumnsForATable(t *testing.T) {
	lines := make([]pdfTextRect, 0, 4)
	for row := 0; row < 2; row++ {
		top := 131.2 + float64(row)*18
		lines = append(lines,
			pdfTestLine(top, 60, 11, 11, fmt.Sprintf("LEFT column sentence %d: lorem ipsum dolor sit amet, consectetur adipiscing elit sed do eiusmod tempor.", row)),
			pdfTestLine(top, 320, 11, 11, fmt.Sprintf("RIGHT column sentence %d: ut labore et dolore magna aliqua ut enim ad minim veniam quis.", row)),
		)
	}
	markdown := pdfTestMarkdown(t, pdfTestPage(lines...), core.PDFOptions{}, core.TableFormatMarkdown)
	if strings.Contains(markdown, "|") {
		t.Fatalf("two-column body text must not become a table: %q", markdown)
	}
	if !strings.Contains(markdown, "LEFT column sentence 0") || !strings.Contains(markdown, "RIGHT column sentence 0") {
		t.Fatalf("two-column text was lost: %q", markdown)
	}
}

func TestPDFLayoutNormalizesListMarkers(t *testing.T) {
	html := pdfTestPage(
		pdfTestLine(131.2, 72, 11, 11, "1. First bullet item"),
		pdfTestLine(149.2, 72, 11, 11, "2. Second bullet item"),
		pdfTestLine(167.2, 72, 11, 11, "3、Third bullet item"),
		pdfTestLine(220, 72, 11, 11, "• Fourth bullet item"),
	)
	want := "1. First bullet item\n2. Second bullet item\n3. Third bullet item\n\n- Fourth bullet item"
	if got := pdfTestMarkdown(t, html, core.PDFOptions{}, core.TableFormatMarkdown); got != want {
		t.Fatalf("unexpected list rendering:\n%q\nwant:\n%q", got, want)
	}
}

func TestPDFLayoutSeparatesAdjacentImagesWithBlankLines(t *testing.T) {
	lines := buildPDFTextLines(pdfTestPage(pdfTestLine(90, 72, 11, 11, "Three images below:")), pdfTestPageHeight)
	markdown, stats, err := renderPDFItems(
		context.Background(),
		pdfTestImageRenderer,
		0,
		lines,
		[]pdfPlacedImage{
			pdfTestPlacedImage(120, 72, 100, 50),
			pdfTestPlacedImage(180, 72, 100, 50),
		},
		core.NormalizePDFOptions(core.PDFOptions{}),
		core.TableFormatMarkdown,
		func(_ context.Context, image core.Image) (string, error) {
			return "assets/" + image.Name, nil
		},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats.imageCount != 2 || !stats.text {
		t.Fatalf("unexpected page stats: %#v", stats)
	}
	want := "Three images below:\n\n![](assets/pdf-page-1-image-1.png)\n\n![](assets/pdf-page-1-image-2.png)"
	if markdown != want {
		t.Fatalf("images were not separated:\n%q\nwant:\n%q", markdown, want)
	}
}

// TestPDFPageDecorationDetection pins the rule that keeps a drawing sheet whose
// text was converted to curves from being reduced to the stamp in its title
// block: a text-less page is decoration only while its pictures stay small
// against the page.
func TestPDFPageDecorationDetection(t *testing.T) {
	const (
		pageWidth  = 595.0
		pageHeight = pdfTestPageHeight
	)
	text := buildPDFTextLines(pdfTestPage(pdfTestLine(90, 72, 11, 11, "Sheet title")), pageHeight)
	tests := []struct {
		name   string
		lines  []pdfTextLine
		images []pdfPlacedImage
		want   bool
	}{
		{
			name:   "stamp on an image-less sheet",
			images: []pdfPlacedImage{pdfTestPlacedImage(700, 270, 72, 36)},
			want:   true,
		},
		{
			name:   "picture covering the page",
			images: []pdfPlacedImage{pdfTestPlacedImage(200, 72, 400, 400)},
			want:   false,
		},
		{
			name:   "picture smaller than a tenth of the page",
			images: []pdfPlacedImage{pdfTestPlacedImage(500, 72, 160, 160)},
			want:   true,
		},
		{
			name:   "picture just above the decoration share",
			images: []pdfPlacedImage{pdfTestPlacedImage(400, 72, 317, 317)},
			want:   false,
		},
		{
			name:   "picture just below the decoration share",
			images: []pdfPlacedImage{pdfTestPlacedImage(400, 72, 316, 316)},
			want:   true,
		},
		{
			name:   "text page with a stamp",
			lines:  text,
			images: []pdfPlacedImage{pdfTestPlacedImage(700, 270, 72, 36)},
			want:   false,
		},
		{
			name: "page without pictures",
			want: false,
		},
		{
			name:   "picture without a usable placement rectangle",
			images: []pdfPlacedImage{{PixelWidth: 8, PixelHeight: 4, MIMEType: "image/png"}},
			want:   false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pageIsDecorationOnly(test.lines, test.images, pageWidth, pageHeight); got != test.want {
				t.Fatalf("pageIsDecorationOnly() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPDFLayoutBuildsLinesAndPlacesImages(t *testing.T) {
	lines := buildPDFTextLines(pdfTestPage(pdfTestLine(90, 72, 11, 11, "Caption text")), pdfTestPageHeight)
	if len(lines) != 1 {
		t.Fatalf("unexpected lines: %#v", lines)
	}
	// The fixture coordinate is a distance from the top of the page; the builder
	// must flip PDF space back into that convention.
	if lines[0].Top != 90 || lines[0].Left != 72 || lines[0].FontSize != 11 {
		t.Fatalf("unexpected line geometry: %#v", lines[0])
	}
	if lines[0].Text() != "Caption text" {
		t.Fatalf("unexpected line text: %q", lines[0].Text())
	}

	placed := orderPDFImages(lines, []pdfPlacedImage{pdfTestPlacedImage(120, 72, 40, 20)})
	if len(placed) != 1 || placed[0].ordinal != 1 {
		t.Fatalf("image was not placed after the line above it: %#v", placed)
	}
}

func TestPDFLayoutMergesOneLineButKeepsColumnsApart(t *testing.T) {
	// Two touching runs on one line are one paragraph line with two spans.
	merged := buildPDFTextLines([]pdfTextRect{
		pdfTestLine(120, 72, 11, 11, "Label: "),
		pdfTestLine(120, 108, 11, 11, "value"),
	}, pdfTestPageHeight)
	if len(merged) != 1 || merged[0].Text() != "Label: value" {
		t.Fatalf("adjacent runs were not merged into one line: %#v", merged)
	}

	// A comma's bounding box sits below the digits around it but it still
	// belongs to the same line.
	lopsided := buildPDFTextLines([]pdfTextRect{
		{Text: "January 26", Left: 100, Right: 150, Top: 700, Bottom: 690, FontSize: 9},
		{Text: ", ", Left: 152, Right: 155, Top: 695, Bottom: 688, FontSize: 9},
		{Text: "2026", Left: 157, Right: 180, Top: 700, Bottom: 690, FontSize: 9},
	}, pdfTestPageHeight)
	if len(lopsided) != 1 || lopsided[0].Text() != "January 26, 2026" {
		t.Fatalf("runs sharing a baseline were split: %#v", lopsided)
	}

	// A column gap stays inside the line as a second run. Splitting it into two
	// lines here is exactly what would turn a letter spaced title into one
	// paragraph per glyph; the block stage decides what the gap means.
	apart := buildPDFTextLines([]pdfTextRect{
		pdfTestLine(120, 72, 11, 11, "Left"),
		pdfTestLine(120, 320, 11, 11, "Right"),
	}, pdfTestPageHeight)
	if len(apart) != 1 || len(apart[0].Spans) != 2 {
		t.Fatalf("the column gap was lost: %#v", apart)
	}
	if cells := pdfLineCells(apart[0]); len(cells) != 2 {
		t.Fatalf("the column gap did not become two cells: %#v", cells)
	}

	// A different baseline must never merge, however close it is.
	stacked := buildPDFTextLines([]pdfTextRect{
		pdfTestLine(120, 72, 11, 11, "first"),
		pdfTestLine(131, 72, 11, 11, "second"),
	}, pdfTestPageHeight)
	if len(stacked) != 2 {
		t.Fatalf("separate lines were merged: %#v", stacked)
	}
}

// TestPDFLayoutEscapesMarkdownMarkers covers what routing the text through lute
// buys: a PDF that literally contains Markdown markers must not turn into
// emphasis, code or a math block.
func TestPDFLayoutEscapesMarkdownMarkers(t *testing.T) {
	markdown := pdfTestMarkdown(t, []pdfTextRect{
		{Text: "2 * 3 = 6", Left: 72, Right: 200, Top: 700, Bottom: 689, FontSize: 11},
		{Text: "snake_case and $var and a~b", Left: 72, Right: 320, Top: 680, Bottom: 669, FontSize: 11},
	}, core.PDFOptions{DisableParagraphReflow: true}, core.TableFormatMarkdown)
	for _, want := range []string{`2 \* 3 = 6`, `snake\_case`, `\$var`, `a\~b`} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("missing %q in:\n%s", want, markdown)
		}
	}
}

func TestPDFLayoutMapsFontMetricsToEmphasis(t *testing.T) {
	lines := buildPDFTextLines([]pdfTextRect{
		{Text: "bold", Left: 72, Right: 100, Top: 700, Bottom: 689, FontSize: 11, Weight: 700},
		{Text: "italic", Left: 72, Right: 110, Top: 680, Bottom: 669, FontSize: 11, Flags: fontFlagItalic},
		{Text: "plain", Left: 72, Right: 110, Top: 660, Bottom: 649, FontSize: 11, Weight: 400},
	}, pdfTestPageHeight)
	markdown, err := joinPDFBlocks(buildPDFLineBlocks(lines, nil, core.NormalizePDFOptions(core.PDFOptions{}), core.TableFormatMarkdown))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**bold**", "*italic*", "plain"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("missing %q in %q", want, markdown)
		}
	}
}

func TestPDFPageRangeResolvesAgainstPageCount(t *testing.T) {
	options := core.NormalizePDFOptions(core.PDFOptions{FirstPage: 2, LastPage: 3})
	first, last, err := options.PageRange(5)
	if err != nil || first != 1 || last != 3 {
		t.Fatalf("unexpected range first=%d last=%d err=%v", first, last, err)
	}
	if _, _, err := options.PageRange(1); err == nil {
		t.Fatal("a range past the last page must fail")
	}
}

// ---- fixtures -------------------------------------------------------------

// pdfTestPageHeight is the A4 height of the synthetic pages. Fixtures take a
// distance from the top of the page, which buildPDFTextLines flips into PDF
// space and back again.
const pdfTestPageHeight = 842.0

// pdfTestPage collects the text rectangles of one synthetic page.
func pdfTestPage(parts ...pdfTextRect) []pdfTextRect { return parts }

// pdfTestLine builds one positioned text rectangle, using the same
// (top, left, size, lineHeight) values the previous HTML fixtures took.
func pdfTestLine(top, left, size, lineHeight float64, text string) pdfTextRect {
	width := float64(utf8.RuneCountInString(text)) * size * 0.5
	return pdfTextRect{
		Text:     text,
		Left:     left,
		Right:    left + width,
		Top:      pdfTestPageHeight - top,
		Bottom:   pdfTestPageHeight - top - lineHeight,
		FontSize: size,
	}
}

// pdfTestPlacedImage builds one image object placed a distance from page top.
func pdfTestPlacedImage(top, left, width, height float64) pdfPlacedImage {
	return pdfPlacedImage{
		Left:        left,
		Right:       left + width,
		Bottom:      pdfTestPageHeight - top - height,
		Top:         pdfTestPageHeight - top,
		TopDown:     top,
		PixelWidth:  uint(width * 2),
		PixelHeight: uint(height * 2),
	}
}

// pdfTestImageRenderer stands in for the PDF engine when a layout test needs
// images without opening a real document.
func pdfTestImageRenderer(_ int, _ pdfPlacedImage, _ core.PDFOptions) ([]byte, string, error) {
	return []byte{0x89, 'P', 'N', 'G'}, "image/png", nil
}

// pdfTestMarkdown renders the text blocks of one synthetic page.
func pdfTestMarkdown(t *testing.T, rects []pdfTextRect, options core.PDFOptions, tableFormat core.TableFormat) string {
	t.Helper()
	lines := buildPDFTextLines(rects, pdfTestPageHeight)
	markdown, err := joinPDFBlocks(buildPDFLineBlocks(lines, nil, core.NormalizePDFOptions(options), tableFormat))
	if err != nil {
		t.Fatal(err)
	}
	return markdown
}
