package render

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"unicode/utf16"

	"github.com/hkmt-sw/markout/internal/mathml"
)

// A PDF says which character each glyph stands for in a "ToUnicode" map,
// which is what copying, searching and screen readers go by. gopdf writes a
// character beyond the first 65536 (the italic letters of formulas are
// there) as its number, where the map wants the two halves it has in
// UTF-16; readers then take the number for another character. The maps are
// put right in the finished file, and since that changes their length, the
// file's table of where its objects are is brought up to date.
//
// The italic and bold letters of formulas are given as the plain letters
// they are, the way TeX's PDFs have them: a search for "mc" finds the
// formula, and a screen reader says "m c".

var (
	// One map: "<< /Length n >> stream ... endcmap ... end end"
	toUnicodeMap = regexp.MustCompile(`(?s)<<\n/Length (\d+)\n>>\nstream\n(/CIDInit /ProcSet findresource begin\n.*?endcmap CMapName currentdict /CMap defineresource pop end end\n)`)
	// A glyph and its character
	glyphChar = regexp.MustCompile(`(<[0-9A-F]{4}><[0-9A-F]{4}>)<([0-9A-F]{4,6})>`)
	xrefHead  = regexp.MustCompile(`\nxref\n0 (\d+)\n`)
	xrefTail  = regexp.MustCompile(`startxref\n(\d+)\n%%EOF\s*$`)
)

// fixToUnicode rewrites the characters beyond the first 65536 in the PDF's
// ToUnicode maps as UTF-16 pairs. A file it does not recognize as gopdf's is
// returned as it is.
func fixToUnicode(pdf []byte) []byte {
	type edit struct {
		at, length int // the bytes replaced
		with       []byte
	}
	var edits []edit
	for _, m := range toUnicodeMap.FindAllSubmatchIndex(pdf, -1) {
		body := pdf[m[4]:m[5]]
		fixed := glyphChar.ReplaceAllFunc(body, func(entry []byte) []byte {
			parts := glyphChar.FindSubmatch(entry)
			code, err := strconv.ParseUint(string(parts[2]), 16, 32)
			if err != nil || code > 0x10FFFF {
				return entry
			}
			c := rune(code)
			switch base, variant := mathml.Unstyle(c); variant {
			case mathml.Italic, mathml.Bold, mathml.BoldItalic:
				c = base
			}
			if c < 0x10000 {
				return []byte(fmt.Sprintf("%s<%04X>", parts[1], c))
			}
			hi, lo := utf16.EncodeRune(c)
			return []byte(fmt.Sprintf("%s<%04X%04X>", parts[1], hi, lo))
		})
		if bytes.Equal(fixed, body) {
			continue
		}
		edits = append(edits,
			edit{m[2], m[3] - m[2], []byte(strconv.Itoa(len(fixed)))}, // the stream's length
			edit{m[4], len(body), fixed})
	}
	if len(edits) == 0 {
		return pdf
	}

	head, tail := xrefHead.FindSubmatchIndex(pdf), xrefTail.FindSubmatchIndex(pdf)
	if head == nil || tail == nil {
		return pdf
	}
	count, _ := strconv.Atoi(string(pdf[head[2]:head[3]]))
	entries := head[1] // the entries follow the head, twenty bytes each
	if entries+20*count > len(pdf) {
		return pdf
	}
	// moved says where a place in the old file is in the new one
	moved := func(offset int) int {
		shift := 0
		for _, e := range edits {
			if e.at < offset {
				shift += len(e.with) - e.length
			}
		}
		return offset + shift
	}

	var out bytes.Buffer
	at := 0
	write := func(upTo int) { out.Write(pdf[at:upTo]); at = upTo }
	for _, e := range edits {
		write(e.at)
		out.Write(e.with)
		at += e.length
	}
	// The table of objects: each entry is an offset of ten digits
	write(entries)
	for i := 0; i < count; i++ {
		entry := pdf[entries+20*i : entries+20*i+20]
		offset, err := strconv.Atoi(string(entry[:10]))
		if err != nil {
			return pdf
		}
		if entry[17] == 'n' {
			offset = moved(offset)
		}
		fmt.Fprintf(&out, "%010d", offset)
		out.Write(entry[10:])
	}
	at = entries + 20*count
	write(tail[2])
	start, _ := strconv.Atoi(string(pdf[tail[2]:tail[3]]))
	out.WriteString(strconv.Itoa(moved(start)))
	at = tail[3]
	write(len(pdf))
	return out.Bytes()
}
