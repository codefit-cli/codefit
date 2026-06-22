package typescript

import (
	ts "github.com/odvcencio/gotreesitter"

	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// tsNode adapts a gotreesitter node to the core's parser-agnostic syntax.Node.
// It carries the language (gotreesitter threads it through node methods) and the
// source bytes (for Text), so the core never sees either.
type tsNode struct {
	n    *ts.Node
	lang *ts.Language
	src  []byte
}

// wrap adapts a gotreesitter node, returning a nil syntax.Node for a nil node so
// callers get a true nil interface (e.g. ChildByField on an absent field).
func (w tsNode) wrap(n *ts.Node) syntax.Node {
	if n == nil {
		return nil
	}
	return tsNode{n: n, lang: w.lang, src: w.src}
}

func (w tsNode) Type() string { return w.n.Type(w.lang) }

func (w tsNode) Text() []byte { return []byte(w.n.Text(w.src)) }

func (w tsNode) NamedChildCount() int { return w.n.NamedChildCount() }

func (w tsNode) NamedChild(i int) syntax.Node { return w.wrap(w.n.NamedChild(i)) }

func (w tsNode) ChildByField(name string) syntax.Node {
	return w.wrap(w.n.ChildByFieldName(name, w.lang))
}

func (w tsNode) StartLine() int { return int(w.n.StartPoint().Row) + 1 }

func (w tsNode) StartByte() int { return int(w.n.StartByte()) }

func (w tsNode) EndByte() int { return int(w.n.EndByte()) }

func (w tsNode) HasError() bool { return w.n.HasError() }
