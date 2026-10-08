package render

import (
	"fmt"

	"github.com/hkmt-sw/markout/internal/mathml"
)

// mathProblems collects the formulas that could not be typeset, which are
// shown as their LaTeX source instead.
type mathProblems struct {
	first string
	count int
	seen  map[string]bool
}

// note records a formula that failed, once however often it is laid out.
func (p *mathProblems) note(tex string, err error) {
	if p.seen == nil {
		p.seen = map[string]bool{}
	}
	if p.seen[tex] {
		return
	}
	p.seen[tex] = true
	if p.count == 0 {
		p.first = mathml.Describe(tex, err)
	}
	p.count++
}

// warning says what happened, or "" if every formula was typeset.
func (p *mathProblems) warning() string {
	switch p.count {
	case 0:
		return ""
	case 1:
		return "1 formula could not be typeset and is shown as its source: " + p.first
	}
	return fmt.Sprintf("%d formulas could not be typeset and are shown as their source; the first: %s", p.count, p.first)
}
