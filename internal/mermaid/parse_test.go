package mermaid

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// describe writes a graph as its nodes and connections, one per line.
func describe(g *Graph) string {
	shapes := map[Shape]string{Rect: "rect", Round: "round", Stadium: "stadium", Subroutine: "subroutine", Cylinder: "cylinder",
		Circle: "circle", Diamond: "diamond", Hexagon: "hexagon", Parallelogram: "parallelogram", Trapezoid: "trapezoid", Flag: "flag"}
	lines := map[Line]string{Solid: "-", Dotted: ".", Thick: "=", Invisible: "~"}
	tips := map[Tip]string{NoTip: "", Arrow: ">", Dot: "o", Cross: "x"}
	var b strings.Builder
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "%s %s %q", n.ID, shapes[n.Shape], strings.Join(n.Label, "/"))
		if n.Group >= 0 {
			fmt.Fprintf(&b, " in %s", g.Groups[n.Group].ID)
		}
		b.WriteString("\n")
	}
	for _, e := range g.Edges {
		start := tips[e.Start]
		if start == ">" {
			start = "<"
		}
		fmt.Fprintf(&b, "%s %s%s%s %s", g.Nodes[e.From].ID, start, strings.Repeat(lines[e.Line], e.Length), tips[e.End], g.Nodes[e.To].ID)
		if len(e.Label) > 0 {
			fmt.Fprintf(&b, " %q", strings.Join(e.Label, "/"))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestParse(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"two nodes", "graph TD\nA --> B\n", "A rect \"A\"\nB rect \"B\"\nA -> B\n"},
		{"flowchart keyword, semicolons", "flowchart LR; A-->B; B-->C;", "A rect \"A\"\nB rect \"B\"\nC rect \"C\"\nA -> B\nB -> C\n"},
		{"shapes", "graph TD\nA[Box] --> B(Round)\nC([Stadium]) --> D[[Sub]]\nE[(Data)] --> F((Circle))\nG{Choice} --> H{{Hex}}\nI[/Par/] --> J[/Trap\\]\nK>Flag]\n",
			"A rect \"Box\"\nB round \"Round\"\nC stadium \"Stadium\"\nD subroutine \"Sub\"\nE cylinder \"Data\"\nF circle \"Circle\"\nG diamond \"Choice\"\nH hexagon \"Hex\"\n" +
				"I parallelogram \"Par\"\nJ trapezoid \"Trap\"\nK flag \"Flag\"\nA -> B\nC -> D\nE -> F\nG -> H\nI -> J\n"},
		{"chain", "graph TD\nA --> B --> C\n", "A rect \"A\"\nB rect \"B\"\nC rect \"C\"\nA -> B\nB -> C\n"},
		{"fan out", "graph TD\nA & B --> C & D\n", "A rect \"A\"\nB rect \"B\"\nC rect \"C\"\nD rect \"D\"\nA -> C\nA -> D\nB -> C\nB -> D\n"},
		{"kinds of line", "graph TD\nA --- B\nA -.-> C\nA ==> D\nA -.- E\nA === F\nA ~~~ G\n",
			"A rect \"A\"\nB rect \"B\"\nC rect \"C\"\nD rect \"D\"\nE rect \"E\"\nF rect \"F\"\nG rect \"G\"\nA - B\nA .> C\nA => D\nA . E\nA = F\nA ~ G\n"},
		{"tips", "graph TD\nA --o B\nA --x C\nA <--> D\nA o--o E\nA x--x F\n",
			"A rect \"A\"\nB rect \"B\"\nC rect \"C\"\nD rect \"D\"\nE rect \"E\"\nF rect \"F\"\nA -o B\nA -x C\nA <-> D\nA o-o E\nA x-x F\n"},
		{"text on a line", "graph TD\nA -->|yes| B\nA -- no --> C\nA -. maybe .-> D\nA == sure ==> E\nA ---|plain| F\nA -- two words --- G\n",
			"A rect \"A\"\nB rect \"B\"\nC rect \"C\"\nD rect \"D\"\nE rect \"E\"\nF rect \"F\"\nG rect \"G\"\n" +
				"A -> B \"yes\"\nA -> C \"no\"\nA .> D \"maybe\"\nA => E \"sure\"\nA - F \"plain\"\nA - G \"two words\"\n"},
		{"longer lines", "graph TD\nA ---> B\nA ----> C\nA ---- D\nA -..-> E\nA ===> F\n",
			"A rect \"A\"\nB rect \"B\"\nC rect \"C\"\nD rect \"D\"\nE rect \"E\"\nF rect \"F\"\nA --> B\nA ---> C\nA -- D\nA ..> E\nA ==> F\n"},
		{"no spaces", "graph TD\nA-->B\nB-->|x|C\nC-.->D\n", "A rect \"A\"\nB rect \"B\"\nC rect \"C\"\nD rect \"D\"\nA -> B\nB -> C \"x\"\nC .> D\n"},
		{"text of nodes", "graph TD\nA[\"Quoted (with) [brackets]\"] --> B[Line one<br/>line two]\nC[\"a #amp; b &lt; c\"] --> D[**bold** `code`]\n",
			"A rect \"Quoted (with) [brackets]\"\nB rect \"Line one/line two\"\nC rect \"a & b < c\"\nD rect \"bold `code\"\nA -> B\nC -> D\n"},
		{"a node defined after it is used", "graph TD\nA --> B\nB[The B]\nA(The A)\n", "A round \"The A\"\nB rect \"The B\"\nA -> B\n"},
		{"ids", "graph TD\nnode_1 --> node-2\nsvc.api --> x1\n", "node_1 rect \"node_1\"\nnode-2 rect \"node-2\"\nsvc.api rect \"svc.api\"\nx1 rect \"x1\"\nnode_1 -> node-2\nsvc.api -> x1\n"},
		{"comments, styles and classes are skipped", "%%{init: {}}%%\ngraph TD\n%% a comment\nA:::warn --> B\nstyle A fill:#f00\nclassDef warn fill:#f00\nclass A warn\nlinkStyle 0 stroke:#f00\nclick A href \"x\"\n",
			"A rect \"A\"\nB rect \"B\"\nA -> B\n"},
		{"subgraphs", "graph TD\nsubgraph one [First]\nA --> B\nsubgraph inner\nC\nend\nend\nsubgraph Second Group\nD\nend\nB --> D\n",
			"A rect \"A\" in one\nB rect \"B\" in one\nC rect \"C\" in inner\nD rect \"D\" in Second Group\nA -> B\nB -> D\n"},
		{"a connection to a subgraph goes to a node in it", "graph TD\nX --> one\nsubgraph one\nA --> B\nend\n", "X rect \"X\"\nA rect \"A\" in one\nB rect \"B\" in one\nX -> A\nA -> B\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := Parse(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if got := describe(g); got != tt.want {
				t.Errorf("got:\n%swant:\n%s", got, tt.want)
			}
		})
	}
}

func TestParseDirection(t *testing.T) {
	for src, want := range map[string]Direction{"graph TD\nA": TopDown, "graph TB\nA": TopDown, "graph\nA": TopDown,
		"flowchart BT\nA": BottomUp, "graph LR\nA": LeftRight, "flowchart RL\nA": RightLeft} {
		g, err := Parse(src)
		if err != nil || g.Dir != want {
			t.Errorf("%q: direction %v, error %v; want %v", src, g, err, want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{"sequenceDiagram\nA->>B: hi", "pie\n\"a\": 1", "", "just text", "gantt\ntitle x"} {
		if _, err := Parse(src); !errors.Is(err, ErrNotFlowchart) {
			t.Errorf("%q: error %v, want ErrNotFlowchart", src, err)
		}
	}
	for src, want := range map[string]string{
		"graph TD\n":                    "empty",
		"graph TD\nA[unclosed --> B":    "not closed",
		"graph TD\nA --> ":              "line 2 (A -->): a connection leads nowhere",
		"graph TD\nA --> B\n\nB ?? C\n": "line 4 (B ?? C)",
		"graph TD\nA ?? B":              "not understood",
		"graph TD\nA@{ shape: rect }":   "not read",
		"graph TD\nA -->|unclosed B":    "not understood",
	} {
		_, err := Parse(src)
		if err == nil || errors.Is(err, ErrNotFlowchart) || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want one mentioning %q", src, err, want)
		}
	}
	big := "graph TD\n" + strings.Repeat("a --> b\n", MaxEdges+1)
	if _, err := Parse(big); err == nil {
		t.Error("a chart beyond the limits was read")
	}
}

func TestKind(t *testing.T) {
	for src, want := range map[string]string{"graph TD\nA": "graph", "%% c\n\nsequenceDiagram\n": "sequenceDiagram",
		"pie title Pets": "pie", "flowchart LR;A": "flowchart", "": "", "stateDiagram-v2\n": "stateDiagram-v2"} {
		if got := Kind(src); got != want {
			t.Errorf("%q: kind %q, want %q", src, got, want)
		}
	}
}
