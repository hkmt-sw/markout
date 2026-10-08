package render

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/hkmt-sw/markout/internal/ast"
)

// anchors are the headings a link within the document can go to: what the
// link may say after "#", and the ID of the heading that means. A heading
// answers to its ID and to the anchor GitHub and GitLab give it, which keeps
// accented and other non-ASCII letters.
type anchors map[string]string

// collectAnchors finds the headings that have an ID, in the order they are
// rendered, and what links may call them. It goes where the renderers find
// headings: the document itself and the blocks of list items.
func collectAnchors(elems []ast.Element) (anchors, []ast.Heading) {
	found := anchors{}
	var headings []ast.Heading
	slugs := map[string]int{} // how many headings share a slug so far

	var walk func(elems []ast.Element)
	var walkItems func(items []ast.ListItem)
	walk = func(elems []ast.Element) {
		for _, elem := range elems {
			switch e := elem.(type) {
			case ast.Heading:
				if e.ID == "" {
					continue
				}
				if _, seen := found[e.ID]; seen {
					continue // a link can only go to the first of them
				}
				headings = append(headings, e)
				found[e.ID] = e.ID
			case ast.List:
				walkItems(e.Items)
			}
		}
	}
	walkItems = func(items []ast.ListItem) {
		for _, item := range items {
			walk(item.Blocks)
			walkItems(item.Children)
		}
	}
	walk(elems)

	// IDs come first, so a slug never takes a name that is another heading's ID
	for _, h := range headings {
		base := headingSlug(h.Runs)
		slug := base
		if n := slugs[base]; n > 0 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		slugs[base]++
		if _, taken := found[slug]; !taken && slug != "" {
			found[slug] = h.ID
		}
	}
	return found, headings
}

// resolve returns the ID of the heading a link such as "#my-heading" goes
// to. ok is false for links to anywhere else.
func (a anchors) resolve(link string) (id string, ok bool) {
	if !strings.HasPrefix(link, "#") {
		return "", false
	}
	name := link[1:]
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	if id, ok = a[name]; ok {
		return id, true
	}
	id, ok = a[strings.ToLower(name)]
	return id, ok
}

// headingSlug is the anchor GitHub gives a heading: its text in lower case,
// with spaces as hyphens and punctuation dropped.
func headingSlug(runs []ast.InlineRun) string {
	var b strings.Builder
	for _, run := range runs {
		for _, c := range strings.ToLower(run.Text) {
			switch {
			case unicode.IsLetter(c) || unicode.IsDigit(c) || c == '-' || c == '_':
				b.WriteRune(c)
			case c == ' ':
				b.WriteByte('-')
			}
		}
	}
	return b.String()
}

// headingText is a heading's text without its formatting.
func headingText(runs []ast.InlineRun) string {
	var b strings.Builder
	for _, run := range runs {
		b.WriteString(run.Text)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
