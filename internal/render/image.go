package render

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/hkmt-sw/markout/internal/theme"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	resvg "github.com/kanrichan/resvg-go"
)

// font-family declarations in attribute (font-family="…") and CSS/inline-style
// (font-family: …) form. resvg only renders <text> when the requested family
// resolves to a loaded font, and it does not fall back to the default family
// for an unmatched one — so we rewrite every declaration to our embedded fonts.
var (
	svgFontAttrRe = regexp.MustCompile(`(?i)font-family\s*=\s*("[^"]*"|'[^']*')`)
	svgFontCSSRe  = regexp.MustCompile(`(?i)font-family\s*:\s*[^;}"']*`)
)

// Options tune how a document is rendered.
type Options struct {
	// BaseDir resolves relative image paths, typically the directory of the
	// source Markdown file.
	BaseDir string
	// RemoteImages allows images referenced by http(s) URL to be downloaded
	// from the servers named in RemoteHosts. When false they are left as
	// placeholders and nothing is fetched.
	RemoteImages bool
	// RemoteHosts are the servers images may be downloaded from, as
	// RemoteHostOf names them: the ones the user was shown and agreed to.
	// An image on any other server is left as a placeholder, and so is one
	// that a redirect sends to another server. With none, nothing is
	// downloaded even if RemoteImages is set.
	RemoteHosts []string
	// Theme is how the document looks; nil means the default theme.
	Theme *theme.Set
	// Lang is the language of the document as a normalized language tag
	// ("hu", "en-GB"), or empty if it is not known. It is written into the
	// output for the programs that read it.
	Lang string
	// PDFA asks for a PDF/A-2u file, in PDF output. A pdfa key in the front
	// matter that is true or false decides instead. DOCX output ignores it.
	PDFA bool
	// Warn, if set, is called with each thing the output does not show the
	// way the document has it, such as characters no font can draw.
	Warn func(message string)
}

// theme returns the theme to render with.
func (o Options) theme() theme.Set {
	if o.Theme != nil {
		return *o.Theme
	}
	return theme.Default()
}

// ErrRemoteImageSkipped is returned for an image that would have to be
// downloaded when Options.RemoteImages is off.
var ErrRemoteImageSkipped = errors.New("remote image not loaded")

// ErrRemoteHostNotAllowed is returned for an image on a server that is not in
// Options.RemoteHosts, whether the document names it or a redirect does.
var ErrRemoteHostNotAllowed = errors.New("remote image host not allowed")

// maxRedirects is how many redirects an image download follows, which is what
// net/http does when it is not told otherwise.
const maxRedirects = 10

// IsRemoteURL reports whether an image reference is fetched over the network.
func IsRemoteURL(url string) bool {
	l := strings.ToLower(url)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

// RemoteHostOf names the server an image URL is downloaded from: the host of
// the URL in lower case, with the port if one is given. It is what the user is
// shown before agreeing to a download, and what Options.RemoteHosts holds. A
// reference with no host in it is named by itself.
func RemoteHostOf(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return rawURL
}

// checkRemoteHost refuses a URL that is not an http(s) address on one of the
// servers the user agreed to.
func (o Options) checkRemoteHost(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: %q is not an http(s) address", ErrRemoteHostNotAllowed, u.Scheme)
	}
	host := strings.ToLower(u.Host)
	if host == "" || !slices.ContainsFunc(o.RemoteHosts, func(h string) bool { return strings.ToLower(h) == host }) {
		return fmt.Errorf("%w: %s", ErrRemoteHostNotAllowed, u.Host)
	}
	return nil
}

// fetchRemoteImage downloads the image at rawURL, if the user agreed to
// downloads from its server. A redirect is followed only to a server the user
// agreed to as well: a server that was shown to the user must not be able to
// send the download to an address on the local network that was not.
func fetchRemoteImage(rawURL string, opts Options) ([]byte, error) {
	if !opts.RemoteImages {
		return nil, ErrRemoteImageSkipped
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if err := opts.checkRemoteHost(req.URL); err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return opts.checkRemoteHost(next.URL)
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image: status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes))
}

// looksLikeSVG reports whether data appears to be an SVG document.
func looksLikeSVG(data []byte) bool {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return strings.Contains(strings.ToLower(string(head)), "<svg")
}

// loadRasterImage loads the image at url and returns raster bytes in a format
// the downstream encoders understand (PNG/JPEG/GIF). SVG sources are rasterized
// to PNG so they can be embedded too. widthHint (px, 0 = unset) controls the
// SVG raster resolution. Shared by the PDF and DOCX renderers.
//
// displayWidth is the width in pixels the image should be shown at: the hint
// if there is one, otherwise the intrinsic width of an SVG, whose raster is
// rendered larger than that to stay crisp. It is 0 for raster images without
// a hint, which are shown at their pixel size.
func loadRasterImage(url string, opts Options, widthHint int) (data []byte, displayWidth int, err error) {
	data, err = loadImageBytes(url, opts)
	if err != nil {
		return nil, 0, err
	}
	displayWidth = widthHint
	if looksLikeSVG(data) {
		if displayWidth <= 0 {
			if w, _ := svgIntrinsicSize(data); w > 0 {
				displayWidth = int(w + 0.5)
			}
		}
		data, err = rasterizeSVG(data, widthHint)
		if err != nil {
			return nil, 0, err
		}
	}
	return data, displayWidth, nil
}

// rasterizeSVG renders an SVG document (including its <text>) to PNG bytes,
// preserving aspect ratio. It uses resvg compiled to WASM (no cgo) and loads the
// embedded Liberation fonts so text labels are drawn. The output is supersampled
// because the renderers downscale it to fit the page, which keeps it crisp.
func rasterizeSVG(data []byte, widthHint int) ([]byte, error) {
	ctx, err := resvg.NewContext(context.Background())
	if err != nil {
		return nil, err
	}
	defer ctx.Close()

	renderer, err := ctx.NewRenderer()
	if err != nil {
		return nil, err
	}
	defer renderer.Close()

	// resvg drops <text> unless a matching font is available; load the embedded
	// fonts and rewrite the SVG's font-family declarations to point at them.
	for _, f := range [][]byte{fontSansRegular, fontSansBold, fontSansItalic, fontSansBoldItalic, fontMonoRegular} {
		_ = renderer.LoadFontData(f)
	}
	_ = renderer.SetFontFamily("Liberation Sans")
	data = normalizeSVGFonts(data)

	iw, ih := svgIntrinsicSize(data)
	if iw <= 0 || ih <= 0 {
		// Unknown dimensions: render at the SVG's intrinsic size.
		return renderer.Render(data)
	}

	// resvg stretches the SVG to the requested width x height, so the target
	// must keep the intrinsic aspect ratio to avoid distortion.
	base := widthHint
	if base <= 0 {
		base = int(iw)
	}
	const maxWidth = 2400
	w := base * 2 // supersample
	if w > maxWidth {
		w = maxWidth
	}
	if w < 1 {
		w = int(iw)
	}
	h := int(float64(w) * ih / iw)
	if h < 1 {
		h = 1
	}
	return renderer.RenderWithSize(data, uint32(w), uint32(h))
}

// normalizeSVGFonts rewrites every font-family declaration in the SVG to one of
// the embedded families ("Liberation Mono" when the original asked for a
// monospace/courier font, otherwise "Liberation Sans"), so text renders
// deterministically regardless of the fonts named in the source.
func normalizeSVGFonts(data []byte) []byte {
	pick := func(orig string) string {
		l := strings.ToLower(orig)
		if strings.Contains(l, "mono") || strings.Contains(l, "courier") || strings.Contains(l, "consol") {
			return "Liberation Mono"
		}
		return "Liberation Sans"
	}
	data = svgFontAttrRe.ReplaceAllFunc(data, func(m []byte) []byte {
		return []byte(`font-family="` + pick(string(m)) + `"`)
	})
	data = svgFontCSSRe.ReplaceAllFunc(data, func(m []byte) []byte {
		return []byte("font-family:" + pick(string(m)))
	})
	return data
}

// svgIntrinsicSize extracts the intrinsic width/height of an SVG from its root
// element, preferring explicit width/height and falling back to the viewBox.
// Returns 0,0 when no usable dimensions are present.
func svgIntrinsicSize(data []byte) (w, h float64) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "svg" {
			continue
		}
		var ws, hs, vb string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "width":
				ws = a.Value
			case "height":
				hs = a.Value
			case "viewBox":
				vb = a.Value
			}
		}
		if wv, hv := parseSVGLength(ws), parseSVGLength(hs); wv > 0 && hv > 0 {
			return wv, hv
		}
		if vb != "" {
			f := strings.FieldsFunc(vb, func(r rune) bool {
				return r == ' ' || r == ',' || r == '\t' || r == '\n'
			})
			if len(f) == 4 {
				vw, _ := strconv.ParseFloat(f[2], 64)
				vh, _ := strconv.ParseFloat(f[3], 64)
				if vw > 0 && vh > 0 {
					return vw, vh
				}
			}
		}
		return 0, 0
	}
}

// parseSVGLength parses a length attribute like "640", "640px" or "12pt",
// ignoring the unit. Percentages and unparseable values return 0.
func parseSVGLength(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, "%") {
		return 0
	}
	i := 0
	for i < len(s) && (s[i] == '.' || s[i] == '-' || s[i] == '+' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	return v
}
