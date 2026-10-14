package parse

import (
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
)

// A width too big for an int is held to the cap, not wrapped round to a
// negative number.
func TestHugeImageWidthIsCapped(t *testing.T) {
	tests := []struct {
		name, src string
		want      string
	}{
		{"attribute list", "![a](s.png){width=9999999999999999}", "w=10000"},
		{"obsidian", "![a|9999999999999999](s.png)", "w=10000"},
		{"percent", "![a](s.png){width=9999999999999999%}", "w=10000"},
		{"html", `<img src="s.png" width="9999999999999999">`, "w=10000"},
		{"html px", `<img src="s.png" width="9999999999999999px">`, "w=10000"},
		{"html percent", `<img src="s.png" width="9999999999999999%">`, "w=10000"},
		{"html negative", `<img src="s.png" width="-5">`, "w=0"},
		{"html text", `<img src="s.png" width="wide">`, "w=0"},
		{"plain", "![a](s.png){width=300}", "w=300"},
		{"html plain", `<img src="s.png" width="50%">`, "w=300"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseFlavor([]byte(tt.src+"\n"), flavor.Default(), "")
			if err != nil {
				t.Fatal(err)
			}
			if got := dump(doc); !strings.Contains(got, tt.want+"\n") {
				t.Errorf("got %q, want it to have %s", got, tt.want)
			}
		})
	}
}
