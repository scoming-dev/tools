package pdf

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/88250/lute"

	"github.com/scoming-dev/tools/markdown/internal/core"
)

// PDFNoOCRWarning is reported when a PDF carried pages without a text layer.
// Those pages are converted to pictures and linked from the Markdown; this
// converter never performs OCR.
const PDFNoOCRWarning = "markdown: PDF pages without a text layer are stored as images and linked from the Markdown; this converter does not perform OCR."

// PDFPageRenderWarning is reported when pages had to be rasterized because they
// carried neither extractable text nor embedded images.
const PDFPageRenderWarning = "markdown: %d PDF page(s) carried no extractable text or images and were rendered as pictures."

// NewConverter builds the local PDF converter.
func NewConverter(settings *core.Settings) core.Converter {
	return core.NewExtensionConverter(
		[]string{".pdf"},
		[]string{"application/pdf"},
		func(ctx context.Context, data []byte, info core.StreamInfo) (*core.Result, error) {
			return convertPDF(ctx, data, settings)
		},
	)
}

// pdfPageStats records what one page contributed.
type pdfPageStats struct {
	text       bool
	rendered   bool
	imageCount int
}

// convertPDF converts every page locally with MuPDF (through go-fitz). No OCR
// and no remote service are involved: text pages are rebuilt from the engine's
// positioned text runs, image-only pages keep their pictures, and pages that
// carry neither are rasterized.
func convertPDF(ctx context.Context, data []byte, settings *core.Settings) (*core.Result, error) {
	document, err := openPDFDocument(data)
	if err != nil {
		return nil, err
	}
	defer document.Close()

	options := settings.PDF
	imageHandler := settings.ImageHandlerOrDefault()
	tableFormat := core.TableFormatFromContext(ctx)

	pageCount, err := document.PageCount()
	if err != nil {
		return nil, err
	}
	first, last, err := options.PageRange(pageCount)
	if err != nil {
		return nil, err
	}

	pages := make([]string, 0, last-first)
	var (
		textPageCount    int
		renderedPages    int
		embeddedImages   int
		convertedPageNum int
	)
	accumulate := func(pageMarkdown string, stats pdfPageStats) {
		convertedPageNum++
		embeddedImages += stats.imageCount
		if stats.rendered {
			renderedPages++
		}
		if stats.text {
			textPageCount++
		}
		if text := strings.TrimSpace(pageMarkdown); text != "" {
			pages = append(pages, text)
		}
	}

	// MuPDF contexts are not thread safe, so each worker gets its own document
	// over the same input buffer. That rules out the two document-wide image
	// budgets, which are spent in page order: honouring them sequentially keeps
	// the output reproducible.
	workers := options.PageWorkers()
	if workers > 1 && options.MaxImages == 0 && options.MaxRenderedPages == 0 {
		results, err := convertPDFPagesParallel(ctx, data, first, last, options, tableFormat, imageHandler, workers)
		if err != nil {
			return nil, err
		}
		for _, result := range results {
			accumulate(result.markdown, result.stats)
		}
	} else {
		for page := first; page < last; page++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			pageMarkdown, stats, err := convertPDFPage(ctx, document, page, options, tableFormat, imageHandler, embeddedImages, renderedPages)
			if err != nil {
				return nil, err
			}
			accumulate(pageMarkdown, stats)
		}
	}

	contentType := "text"
	if textPageCount == 0 {
		contentType = "image"
	}
	metadata := map[string]string{
		"pdf_content_type":    contentType,
		"pdf_page_count":      strconv.Itoa(pageCount),
		"pdf_embedded_images": strconv.Itoa(embeddedImages),
		"pdf_rendered_pages":  strconv.Itoa(renderedPages),
	}
	if len(pages) != 0 {
		metadata["pdf_converted_pages"] = strconv.Itoa(convertedPageNum)
	}

	warnings := make([]string, 0, 2)
	if renderedPages > 0 {
		warnings = append(warnings, fmt.Sprintf(PDFPageRenderWarning, renderedPages))
	}
	if textPageCount < convertedPageNum {
		warnings = append(warnings, PDFNoOCRWarning)
	}

	title := ""
	for key, value := range document.Metadata() {
		value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
		if value != "" && strings.EqualFold(key, "title") {
			title = value
		}
	}
	return &core.Result{
		Title:    title,
		Markdown: core.JoinBlocks(pages),
		Metadata: metadata,
		Warnings: warnings,
	}, nil
}

// convertPDFPageResult is one converted page, held until the pages can be
// reassembled in their original order.
type convertPDFPageResult struct {
	markdown string
	stats    pdfPageStats
}

// convertPDFPagesParallel converts the page range with one MuPDF context per
// worker: pages never depend on each other. The raster work (decoding, scaling
// and encoding every embedded image, plus whole-page rendering) dominates a
// conversion and parallelises cleanly.
//
// Each worker opens its own document because a fitz.Document is not safe for
// concurrent use; the input buffer is shared and never written.
func convertPDFPagesParallel(
	ctx context.Context,
	data []byte,
	first, last int,
	options core.PDFOptions,
	tableFormat core.TableFormat,
	imageHandler core.ImageHandler,
	workers int,
) ([]convertPDFPageResult, error) {
	pages := last - first
	if workers > pages {
		workers = pages
	}
	results := make([]convertPDFPageResult, pages)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The image handler is caller supplied and used to be called from a single
	// goroutine; keep that guarantee instead of requiring handlers to be thread
	// safe. Only the store call is serialised, never the encoding before it.
	var handlerMutex sync.Mutex
	guardedHandler := func(ctx context.Context, image core.Image) (string, error) {
		handlerMutex.Lock()
		defer handlerMutex.Unlock()
		return imageHandler(ctx, image)
	}

	var (
		nextPage  atomic.Int64
		waitGroup sync.WaitGroup
		failOnce  sync.Once
		failure   error
	)
	fail := func(err error) {
		failOnce.Do(func() {
			failure = err
			cancel()
		})
	}

	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			document, err := openPDFDocument(data)
			if err != nil {
				fail(err)
				return
			}
			defer document.Close()
			for {
				index := int(nextPage.Add(1)) - 1
				if index >= pages || ctx.Err() != nil {
					return
				}
				markdown, stats, err := convertPDFPage(ctx, document, first+index, options, tableFormat, guardedHandler, 0, 0)
				if err != nil {
					fail(err)
					return
				}
				results[index] = convertPDFPageResult{markdown: markdown, stats: stats}
			}
		}()
	}
	waitGroup.Wait()
	if failure != nil {
		return nil, failure
	}
	return results, nil
}

// pdfDecorationPageShare is the share of the page area below which a picture
// drawn on a page without any text counts as decoration rather than as the
// page's content.
//
// Drawing sheets are the reason the threshold exists. A CAD export whose text
// was converted to curves reaches the engine as vector art, which yields no
// text lines at all, and the only image object it reports is the design stamp
// in the title block. Keeping that stamp would drop the sheet it is stamped on,
// so the page is rasterized instead; a scanned page, whose picture covers the
// page, is left as it is.
//
// The two cases are far apart - a title-block stamp covers a few percent of the
// sheet while a page-sized scan covers most of it - so the cutoff only has to
// sit in the wide gap between them, and erring towards rasterizing costs little:
// the picture is still part of the rendered page.
const pdfDecorationPageShare = 0.2

// pageIsDecorationOnly reports whether a page carries no text and nothing but
// decorative pictures, the shape of an image-less drawing sheet that only
// carries a stamp, seal or watermark.
//
// A page with no picture at all is not decoration: it has no content to lose,
// and convertPDFPage already rasterizes it.
func pageIsDecorationOnly(lines []pdfTextLine, images []pdfPlacedImage, width, height float64) bool {
	if len(images) == 0 || width <= 0 || height <= 0 {
		return false
	}
	for _, line := range lines {
		if line.Text() != "" {
			return false
		}
	}
	covered := 0.0
	for _, image := range images {
		imageWidth := image.Right - image.Left
		imageHeight := image.Top - image.Bottom
		if imageWidth <= 0 || imageHeight <= 0 {
			// Without a usable placement rectangle the picture cannot be
			// classified; keep the page as it is.
			return false
		}
		covered += imageWidth * imageHeight
	}
	return covered < width*height*pdfDecorationPageShare
}

// convertPDFPage converts a single page, falling back to a whole-page picture
// when the page has no text and no content picture to keep.
func convertPDFPage(
	ctx context.Context,
	document *pdfDocument,
	page int,
	options core.PDFOptions,
	tableFormat core.TableFormat,
	imageHandler core.ImageHandler,
	usedImages int,
	renderedSoFar int,
) (string, pdfPageStats, error) {
	var stats pdfPageStats

	lines, err := document.PageTextLines(page)
	if err != nil {
		return "", stats, err
	}
	images, err := document.PageImages(page)
	if err != nil {
		return "", stats, err
	}
	// A sheet whose text was converted to curves arrives with no text and only
	// its stamp left; drop the decoration so the page falls through to the
	// whole-page picture below instead of being reduced to a stamp that says
	// nothing about the sheet. Disabling the fallback keeps the old behaviour,
	// because the caller asked for pages without text not to be rasterized.
	if !options.DisablePageRenderFallback {
		if width, height, sizeErr := document.PageSize(page); sizeErr == nil && pageIsDecorationOnly(lines, images, width, height) {
			lines, images = nil, nil
		}
	}
	content, stats, err := renderPDFItems(ctx, document.RenderImageRegion, page, lines, images, options, tableFormat, imageHandler, usedImages)
	if err != nil {
		return "", stats, err
	}

	if strings.TrimSpace(content) != "" {
		return content, stats, nil
	}
	if options.DisablePageRenderFallback {
		return "", stats, nil
	}
	if options.MaxRenderedPages > 0 && renderedSoFar >= options.MaxRenderedPages {
		return "", stats, nil
	}
	rendered, err := renderPDFPageImage(ctx, document, page, imageHandler, options)
	if err != nil {
		return "", stats, err
	}
	stats.rendered = true
	return rendered, stats, nil
}

// pdfImageRenderer rasterises one placed image. It is injected so the layout
// tests can exercise image placement without a live PDF engine.
type pdfImageRenderer func(page int, image pdfPlacedImage, options core.PDFOptions) ([]byte, string, error)

// pdfImageSlot is one placed image together with the number of text lines that
// precede it on the page.
type pdfImageSlot struct {
	image   pdfPlacedImage
	ordinal int
}

// orderPDFImages places every image after the text lines that sit above it, so
// a picture stays next to the content it illustrates.
// orderPDFImages places every image after the text lines that end above it, so
// a picture stays next to the content it illustrates.
//
// The test is whether a line finishes before the image starts, not where its top
// sits. Comparing tops pushes an image that begins level with a row into the row
// above, and the two values are only ever float approximations of each other.
func orderPDFImages(lines []pdfTextLine, images []pdfPlacedImage) []pdfImageSlot {
	slots := make([]pdfImageSlot, 0, len(images))
	for _, image := range images {
		ordinal := 0
		for _, line := range lines {
			if line.Bottom <= image.TopDown {
				ordinal++
			}
		}
		slots = append(slots, pdfImageSlot{image: image, ordinal: ordinal})
	}
	sort.SliceStable(slots, func(left, right int) bool {
		if slots[left].ordinal != slots[right].ordinal {
			return slots[left].ordinal < slots[right].ordinal
		}
		return slots[left].image.TopDown < slots[right].image.TopDown
	})
	return slots
}

// renderPDFItems rebuilds the Markdown of one page from its positioned lines
// and placed images.
//
// Lines are laid out first and images are then re-inserted at the block
// boundary that matches their position on the page. That keeps a table
// containing pictures (signature blocks, part tables) intact instead of being
// torn apart at every image.
func renderPDFItems(
	ctx context.Context,
	renderImages pdfImageRenderer,
	page int,
	lines []pdfTextLine,
	placed []pdfPlacedImage,
	options core.PDFOptions,
	tableFormat core.TableFormat,
	imageHandler core.ImageHandler,
	usedImages int,
) (string, pdfPageStats, error) {
	var stats pdfPageStats
	for _, line := range lines {
		if line.Text() != "" {
			stats.text = true
		}
	}

	slots := orderPDFImages(lines, placed)
	imageOrdinal := make([]int, 0, len(slots))
	for _, slot := range slots {
		imageOrdinal = append(imageOrdinal, slot.ordinal)
	}

	blocks := buildPDFLineBlocks(lines, imageOrdinal, options, tableFormat)
	ordered := make([]string, 0, len(blocks)+len(slots))
	// One engine per page: lute is not documented as safe for concurrent use,
	// and the converter may run on several documents at once.
	engine := lute.New()
	imageIndex := 0
	emittedImages := 0
	emitImages := func(limit int) error {
		for imageIndex < len(slots) && slots[imageIndex].ordinal <= limit {
			slot := slots[imageIndex]
			imageIndex++
			if emittedImages >= options.MaxImagesPerPage {
				continue
			}
			if options.MaxImages > 0 && usedImages+emittedImages >= options.MaxImages {
				continue
			}
			data, mimeType, err := renderImages(page, slot.image, options)
			if err != nil {
				// One picture that cannot be rasterized must not fail the whole
				// document: drawing sets routinely carry specks and masks that
				// have no usable raster size.
				continue
			}
			emittedImages++
			markdown, err := storePDFEmbeddedImage(ctx, imageHandler, &pdfEmbeddedImage{
				MIMEType: mimeType,
				Data:     data,
			}, page, emittedImages)
			if err != nil {
				return err
			}
			ordered = append(ordered, markdown)
		}
		return nil
	}

	consumed := 0
	for _, block := range blocks {
		if err := emitImages(consumed); err != nil {
			return "", stats, err
		}
		markdown, err := renderPDFBlock(engine, block)
		if err != nil {
			return "", stats, err
		}
		if markdown = strings.TrimSpace(markdown); markdown != "" {
			ordered = append(ordered, markdown)
		}
		consumed += block.lines
	}
	if err := emitImages(len(lines)); err != nil {
		return "", stats, err
	}
	stats.imageCount = emittedImages
	return core.JoinBlocks(ordered), stats, nil
}

func storePDFEmbeddedImage(ctx context.Context, imageHandler core.ImageHandler, image *pdfEmbeddedImage, page, index int) (string, error) {
	name := fmt.Sprintf("pdf-page-%d-image-%d%s", page+1, index, core.ImageExtension(image.MIMEType))
	imageURL, err := imageHandler(ctx, core.Image{
		Name:     name,
		MIMEType: image.MIMEType,
		AltText:  image.AltText,
		Data:     image.Data,
	})
	if err != nil {
		return "", fmt.Errorf("markdown: store PDF page %d image %d: %w", page+1, index, err)
	}
	return core.MarkdownImage(image.AltText, imageURL), nil
}

// renderPDFPageImage rasterizes one whole page, used when the page carries no
// extractable text and no image to keep.
func renderPDFPageImage(ctx context.Context, document *pdfDocument, page int, imageHandler core.ImageHandler, options core.PDFOptions) (string, error) {
	data, mimeType, err := document.RenderPageImage(page, options)
	if err != nil {
		return "", err
	}
	altText := fmt.Sprintf("第 %d 页", page+1)
	imageURL, err := imageHandler(ctx, core.Image{
		Name:     fmt.Sprintf("pdf-page-%d%s", page+1, core.ImageExtension(mimeType)),
		MIMEType: mimeType,
		AltText:  altText,
		Data:     data,
	})
	if err != nil {
		return "", fmt.Errorf("markdown: store PDF page %d image: %w", page+1, err)
	}
	return core.MarkdownImage(altText, imageURL), nil
}
