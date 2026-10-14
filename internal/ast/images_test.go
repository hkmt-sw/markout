package ast

import (
	"slices"
	"testing"
)

func TestImagesFindsEveryContainer(t *testing.T) {
	img := func(url string) Image { return NewImage("", url, "") }
	nested := ListItem{
		Blocks: []Element{img("item-block")},
		Children: []ListItem{
			{Blocks: []Element{Blockquote{Elements: []Element{img("child-quote")}}}},
		},
	}
	elems := []Element{
		img("top"),
		NewParagraph(NewInlineRun("text")),
		List{Items: []ListItem{nested}},
		Blockquote{Elements: []Element{img("quote"), Blockquote{Elements: []Element{img("deep-quote")}}}},
		Alert{Elements: []Element{img("alert")}},
		FootnoteDefinition{Elements: []Element{img("footnote")}},
	}
	var got []string
	for _, i := range Images(elems) {
		got = append(got, i.URL)
	}
	want := []string{"top", "item-block", "child-quote", "quote", "deep-quote", "alert", "footnote"}
	if !slices.Equal(got, want) {
		t.Fatalf("Images = %v, want %v", got, want)
	}
}
