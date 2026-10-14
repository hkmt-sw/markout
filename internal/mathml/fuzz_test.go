package mathml

import (
	"fmt"
	"testing"
	"time"
)

// fuzzTimeLimit is how long typesetting one formula may take.
const fuzzTimeLimit = 2 * time.Second

// FuzzMath reads arbitrary text as a LaTeX formula and writes the result as
// Office Math. None of it may panic or hang. Errors are fine.
//
//	go test -run='^$' -fuzz=FuzzMath -fuzztime=60s ./internal/mathml
func FuzzMath(f *testing.F) {
	for _, seed := range []string{
		"",
		`x^2`,
		`\frac{a}{b}`,
		`\sqrt[3]{x+1}`,
		`\sum_{i=1}^{n} i`,
		`\int_0^\infty e^{-x^2}\,dx`,
		`\text{if } x \operatorname{rank} A \mathbb{R}`,
		"a &= b \\\\ &= c",
		"\\begin{gather*} a \\\\ b \\end{gather*}",
		"\\begin{pmatrix} 1 & 0 \\\\ 0 & 1 \\end{pmatrix}",
		"\\begin{cases} x & y \\\\ z \\end{cases}",
		`\left( \frac{1}{2} \right) \boxed{x} \label{eq:1}`,
		`\unknowncommand`,
		`{{{{`,
		`\frac{\frac{\frac{1}{2}}{3}}{4}`,
		`x_1_2^3^4`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, tex string) {
		err := runBounded(fuzzTimeLimit, func() {
			for _, display := range []bool{false, true} {
				root, err := Parse(tex, display)
				if err != nil || root == nil {
					continue
				}
				_ = OMML(root, display)
			}
		})
		if err != nil {
			t.Fatalf("%v\ninput: %q", err, tex)
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
