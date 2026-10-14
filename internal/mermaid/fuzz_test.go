package mermaid

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// fuzzTimeLimit is how long reading, laying out and drawing one chart may take.
const fuzzTimeLimit = 2 * time.Second

// FuzzMermaid reads arbitrary text as a flowchart and, when it is one, lays
// it out and draws it. None of it may panic or hang. Errors are fine.
//
//	go test -run='^$' -fuzz=FuzzMermaid -fuzztime=60s ./internal/mermaid
func FuzzMermaid(f *testing.F) {
	for _, seed := range []string{
		"",
		"graph TD\nA --> B\n",
		"flowchart LR; A-->B; B-->C;",
		"graph TD\nA[Box] --> B(Round)\nC([Stadium]) --> D[[Sub]]\nE[(Data)] --> F((Circle))\nG{Choice} --> H{{Hex}}\nI[/Par/] --> J[/Trap\\]\nK>Flag]\n",
		"graph TD\nA & B --> C & D\nA -.-> C\nA ==> D\nA ~~~ G\nA <--> D\nA o--o E\n",
		"graph TD\nA -->|yes| B\nA -- no --> C\nA -. maybe .-> D\nA == sure ==> E\n",
		"graph TD\nA[\"Quoted (with) [brackets]\"] --> B[Line one<br/>line two]\nC[\"a #amp; b &lt; c\"] --> D[**bold** `code`]\n",
		"graph TD\nA --> B\nB --> C\nC --> A\nA --> A\n",
		"graph TB\nsubgraph one [First]\nA --> B\nsubgraph two\nC --> D\nend\nend\nB --> C\n",
		"graph LR\nstyle A fill:#f9f\nclassDef x fill:#fff\nA:::x --> B\nclick A callback\n",
		"sequenceDiagram\nA->>B: hi\n",
		"%%{init: {}}%%\n---\ntitle: t\n---\ngraph TD\nA-->B",
		"graph TD\n" + strings.Repeat("A --> B --> C --> D\nD --> A\n", 20),
	} {
		f.Add(seed)
	}

	measure := Metrics{
		Width:      func(s string) float64 { return 6 * float64(len([]rune(s))) },
		LineHeight: 12,
	}
	style := Style{Font: "x", FontSize: 10, NodeFill: "#fff", NodeStroke: "#000", Text: "#000", Line: "#000", FrameFill: "#eee", FrameStroke: "#999", Page: "#fff"}

	f.Fuzz(func(t *testing.T, src string) {
		err := runBounded(fuzzTimeLimit, func() {
			_ = Kind(src)
			g, err := Parse(src)
			if err != nil {
				return
			}
			for _, dir := range []Direction{g.Dir, TopDown, LeftRight, BottomUp, RightLeft} {
				g.Dir = dir
				d := g.Layout(measure)
				_ = d.SVG(style)
				if !finite(d) {
					panic("the drawing holds a number that is not finite")
				}
			}
		})
		if err != nil {
			t.Fatalf("%v\ninput: %q", err, src)
		}
	})
}

// runBounded runs fn on its own goroutine and reports a panic or a run that
// takes longer than limit as an error.
func runBounded(limit time.Duration, fn func()) error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- fmt.Errorf("panic: %v", p)
			}
		}()
		fn()
		done <- nil
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("still running after %v", limit)
	}
}

// finite reports whether the sizes and the points of a drawing are all real
// numbers.
func finite(d *Drawing) bool {
	ok := func(v ...float64) bool {
		for _, x := range v {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return false
			}
		}
		return true
	}
	if !ok(d.W, d.H) {
		return false
	}
	for _, b := range d.Boxes {
		for _, p := range b.Outline {
			if !ok(p.X, p.Y) {
				return false
			}
		}
	}
	for _, l := range d.Links {
		for _, seg := range l.Curve {
			for _, p := range seg {
				if !ok(p.X, p.Y) {
					return false
				}
			}
		}
	}
	return true
}
