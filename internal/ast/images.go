package ast

// Images returns every image in elems, in document order, wherever it sits:
// among the elements themselves or inside a list item (and the lists nested
// in it), a quote, an alert or a footnote.
//
// It is the one place that knows where images can be. The list of servers a
// document downloads from, which the user agrees to before anything is
// fetched, is made from it, so a container added to the tree has to be walked
// here and cannot be missed by that list. It finds a superset of what a
// renderer draws: an image in a place no renderer draws is listed and never
// fetched, which is the safe way round.
func Images(elems []Element) []Image {
	var out []Image
	collectImages(&out, elems)
	return out
}

func collectImages(out *[]Image, elems []Element) {
	for _, elem := range elems {
		switch e := elem.(type) {
		case Image:
			*out = append(*out, e)
		case List:
			collectItemImages(out, e.Items)
		case Blockquote:
			collectImages(out, e.Elements)
		case Alert:
			collectImages(out, e.Elements)
		case FootnoteDefinition:
			collectImages(out, e.Elements)
		}
	}
}

// collectItemImages walks list items: the blocks of an item come before the
// items nested in it, as they are shown.
func collectItemImages(out *[]Image, items []ListItem) {
	for _, item := range items {
		collectImages(out, item.Blocks)
		collectItemImages(out, item.Children)
	}
}
