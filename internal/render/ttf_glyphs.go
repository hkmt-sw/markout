package render

import (
	"encoding/binary"
	"fmt"
)

// glyphBoxes reads, from a TrueType font, how far each character's outline
// reaches above and below the baseline. Laying out a formula needs that: a
// fraction bar goes just under its numerator, a root sign is as tall as
// what is under it. gopdf gives the width of text but not its height.
type glyphBoxes struct {
	unitsPerEm float64
	longLoca   bool
	loca, glyf []byte
	groups     []cmapGroup // sorted by first
}

// cmapGroup maps a run of characters to a run of glyphs.
type cmapGroup struct {
	first, last rune
	glyph       uint32
}

func newGlyphBoxes(data []byte) (*glyphBoxes, error) {
	tables := map[string][]byte{}
	if len(data) < 12 {
		return nil, fmt.Errorf("not a font")
	}
	n := int(binary.BigEndian.Uint16(data[4:6]))
	for i := 0; i < n; i++ {
		rec := 12 + 16*i
		if rec+16 > len(data) {
			return nil, fmt.Errorf("truncated font")
		}
		offset := int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		length := int(binary.BigEndian.Uint32(data[rec+12 : rec+16]))
		if offset < 0 || length < 0 || offset+length > len(data) {
			return nil, fmt.Errorf("truncated font")
		}
		tables[string(data[rec:rec+4])] = data[offset : offset+length]
	}
	head, cmap := tables["head"], tables["cmap"]
	g := &glyphBoxes{loca: tables["loca"], glyf: tables["glyf"]}
	if len(head) < 54 || len(cmap) < 4 || g.loca == nil || g.glyf == nil {
		return nil, fmt.Errorf("the font has no TrueType outlines")
	}
	g.unitsPerEm = float64(binary.BigEndian.Uint16(head[18:20]))
	g.longLoca = binary.BigEndian.Uint16(head[50:52]) != 0
	if g.unitsPerEm == 0 {
		return nil, fmt.Errorf("the font has no size")
	}

	// The table for all of Unicode if there is one, else the one for its
	// first 65536 characters
	var full, basic []byte
	for i := 0; i < int(binary.BigEndian.Uint16(cmap[2:4])); i++ {
		rec := 4 + 8*i
		if rec+8 > len(cmap) {
			break
		}
		offset := int(binary.BigEndian.Uint32(cmap[rec+4 : rec+8]))
		if offset+4 > len(cmap) {
			continue
		}
		switch binary.BigEndian.Uint16(cmap[offset : offset+2]) {
		case 12:
			full = cmap[offset:]
		case 4:
			basic = cmap[offset:]
		}
	}
	switch {
	case full != nil:
		g.readFormat12(full)
	case basic != nil:
		g.readFormat4(basic)
	default:
		return nil, fmt.Errorf("the font has no character table markout can read")
	}
	return g, nil
}

func (g *glyphBoxes) readFormat12(t []byte) {
	if len(t) < 16 {
		return
	}
	n := int(binary.BigEndian.Uint32(t[12:16]))
	for i := 0; i < n && 16+12*i+12 <= len(t); i++ {
		rec := t[16+12*i:]
		g.groups = append(g.groups, cmapGroup{
			first: rune(binary.BigEndian.Uint32(rec[0:4])),
			last:  rune(binary.BigEndian.Uint32(rec[4:8])),
			glyph: binary.BigEndian.Uint32(rec[8:12]),
		})
	}
}

func (g *glyphBoxes) readFormat4(t []byte) {
	if len(t) < 14 {
		return
	}
	segs := int(binary.BigEndian.Uint16(t[6:8])) / 2
	ends, starts := 14, 16+2*segs
	deltas, offsets := starts+2*segs, starts+4*segs
	if offsets+2*segs > len(t) {
		return
	}
	u16 := func(at int) int { return int(binary.BigEndian.Uint16(t[at : at+2])) }
	for i := 0; i < segs; i++ {
		first, last := u16(starts+2*i), u16(ends+2*i)
		delta, offset := u16(deltas+2*i), u16(offsets+2*i)
		if first == 0xFFFF {
			continue
		}
		if offset == 0 {
			g.groups = append(g.groups, cmapGroup{rune(first), rune(last), uint32((first + delta) & 0xFFFF)})
			continue
		}
		// Glyphs listed one by one
		for c := first; c <= last; c++ {
			at := offsets + 2*i + offset + 2*(c-first)
			if at+2 > len(t) {
				break
			}
			if glyph := u16(at); glyph != 0 {
				g.groups = append(g.groups, cmapGroup{rune(c), rune(c), uint32((glyph + delta) & 0xFFFF)})
			}
		}
	}
}

// glyph returns the number of a character's glyph.
func (g *glyphBoxes) glyph(c rune) (uint32, bool) {
	lo, hi := 0, len(g.groups)
	for lo < hi {
		mid := (lo + hi) / 2
		switch group := g.groups[mid]; {
		case c < group.first:
			hi = mid
		case c > group.last:
			lo = mid + 1
		default:
			return group.glyph + uint32(c-group.first), true
		}
	}
	// Tables are sorted, but a font that is not is still searched
	for _, group := range g.groups {
		if c >= group.first && c <= group.last {
			return group.glyph + uint32(c-group.first), true
		}
	}
	return 0, false
}

// bounds returns how far a character's outline goes below and above the
// baseline, as fractions of the font size; below is negative. ok is false
// for a character the font does not have or that draws nothing.
func (g *glyphBoxes) bounds(c rune) (yMin, yMax float64, ok bool) {
	id, found := g.glyph(c)
	if !found {
		return 0, 0, false
	}
	at := func(i uint32) (int, bool) {
		if g.longLoca {
			if int(i)*4+4 > len(g.loca) {
				return 0, false
			}
			return int(binary.BigEndian.Uint32(g.loca[i*4:])), true
		}
		if int(i)*2+2 > len(g.loca) {
			return 0, false
		}
		return int(binary.BigEndian.Uint16(g.loca[i*2:])) * 2, true
	}
	start, ok1 := at(id)
	end, ok2 := at(id + 1)
	if !ok1 || !ok2 || end <= start || start+10 > len(g.glyf) {
		return 0, 0, false
	}
	header := g.glyf[start:]
	lo := float64(int16(binary.BigEndian.Uint16(header[4:6])))
	hi := float64(int16(binary.BigEndian.Uint16(header[8:10])))
	return lo / g.unitsPerEm, hi / g.unitsPerEm, true
}
