package render

import (
	"math"
	"testing"
)

// FuzzSVGSize reads the intrinsic size of arbitrary bytes taken for an SVG.
// It must not panic, and a size it reports is either "none" (0, 0) or two
// finite numbers above zero: the callers divide by them and scale pages by
// them.
//
//	go test -run='^$' -fuzz=FuzzSVGSize -fuzztime=60s ./internal/render
func FuzzSVGSize(f *testing.F) {
	for _, seed := range []string{
		"",
		`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="50"></svg>`,
		`<svg width="10.5mm" height="2in" viewBox="0 0 10 5"/>`,
		`<svg viewBox="0 0 120 80"/>`,
		`<svg viewBox="0,0,120,80"/>`,
		`<svg width="100%" height="100%" viewBox="0 0 1 1"/>`,
		`<svg width="-5" height="+7"/>`,
		`<svg width="1e400" height="1e400" viewBox="0 0 1e999 1e999"/>`,
		`<svg viewBox="0 0 NaN Inf"/>`,
		`<svg viewBox="0 0 0x1p3 +Inf"/>`,
		`<svg viewBox="0 0 1 -1"/>`,
		`<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY a "b">]><svg width="&a;" height="1"/>`,
		`<a><b><svg width="3" height="4"/></b></a>`,
		`<svg><svg width="3" height="4"/></svg>`,
		`<svg width=`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		w, h := svgIntrinsicSize(data)
		if w == 0 && h == 0 {
			return
		}
		finite := func(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
		if !finite(w) || !finite(h) {
			t.Fatalf("svgIntrinsicSize(%q) = %v, %v", data, w, h)
		}
	})
}
