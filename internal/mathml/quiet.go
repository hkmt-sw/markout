package mathml

import (
	"io"
	"log"
	"os"
	"sync"
	_ "unsafe" // for go:linkname

	"github.com/wyatt915/treeblood"
)

// The translator talks: it logs warnings to standard error, and when it
// fails it prints the failure and the formula to standard output. Neither
// belongs in the output of a program that uses it, least of all one that is
// drawing a screen. It has no setting for either, so both are stopped here.

// Its logger is a variable of its own package, reached by name.
//
//go:linkname translatorLog github.com/wyatt915/treeblood.logger
var translatorLog *log.Logger

func init() {
	if translatorLog != nil {
		translatorLog.SetOutput(io.Discard)
	}
}

var (
	quiet   sync.Mutex
	nowhere = sync.OnceValue(func() *os.File {
		f, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		return f
	})
)

// translate turns LaTeX into MathML text with nothing printed. What the
// translator writes to standard output goes to the null device for the
// length of the call: fmt looks up os.Stdout when it prints, while anything
// that took hold of the real standard output earlier, as a terminal UI
// does, keeps writing to it.
func translate(tex string, display bool) (string, error) {
	quiet.Lock()
	defer quiet.Unlock()
	if null := nowhere(); null != nil {
		stdout := os.Stdout
		os.Stdout = null
		defer func() { os.Stdout = stdout }()
	}
	if display {
		return treeblood.DisplayStyle(tex, nil)
	}
	return treeblood.InlineStyle(tex, nil)
}
