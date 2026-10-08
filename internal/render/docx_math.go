package render

import (
	"fmt"
	"strings"

	"github.com/hkmt-sw/markout/internal/mathml"
)

// A formula goes into a DOCX as a Word equation (Office Math Markup). The
// library has no call for that, so a run holding a mark is written where
// the formula belongs, and the equation takes its place after the document
// is saved.

const mathMarkFormat = "markout-math-%d"

// mathMark typesets a formula and returns the mark that stands for it in
// the document. ok is false if the formula cannot be typeset; it is then
// shown as its source.
func (r *DocxRenderer) mathMark(tex string, display bool) (mark string, ok bool) {
	root, err := mathml.Parse(tex, display)
	if err != nil {
		r.mathProblems.note(tex, err)
		return "", false
	}
	r.equations = append(r.equations, mathml.OMML(root, display))
	return fmt.Sprintf(mathMarkFormat, len(r.equations)-1), true
}

const mathNamespace = `xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"`

// insertMath puts the equations in place of the runs that hold their marks.
func insertMath(content []byte, equations []string) []byte {
	if len(equations) == 0 {
		return content
	}
	str := string(content)
	for i, equation := range equations {
		mark := fmt.Sprintf(mathMarkFormat, i)
		at := strings.Index(str, mark)
		if at < 0 {
			continue
		}
		start := strings.LastIndex(str[:at], "<w:r>")
		end := strings.Index(str[at:], "</w:r>")
		if start < 0 || end < 0 {
			str = str[:at] + str[at+len(mark):]
			continue
		}
		str = str[:start] + equation + str[at+end+len("</w:r>"):]
	}
	if !strings.Contains(str, mathNamespace) {
		str = strings.Replace(str, "<w:document ", "<w:document "+mathNamespace+" ", 1)
	}
	return []byte(str)
}
