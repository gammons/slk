package wintree

// LayoutNode is the renderable shape of the tree: leaves carry their
// window id and rect; splits carry direction and children. The UI
// walks this to compose window panes (it never sees *node).
type LayoutNode struct {
	Leaf     bool
	ID       LeafID
	Rect     Rect
	Dir      Dir
	Children []LayoutNode
}

// Layout resolves every node's rect within bounds. Children of a
// split divide the parent extent equally; remainders go to the
// earliest children one cell each, so rects always tile exactly.
func (t *Tree) Layout(bounds Rect) LayoutNode {
	return layoutNode(t.root, bounds)
}

func layoutNode(n *node, r Rect) LayoutNode {
	if n.isLeaf() {
		return LayoutNode{Leaf: true, ID: n.id, Rect: r}
	}
	out := LayoutNode{Rect: r, Dir: n.dir, Children: make([]LayoutNode, 0, len(n.children))}
	for i, cr := range childRects(n.dir, len(n.children), r) {
		out.Children = append(out.Children, layoutNode(n.children[i], cr))
	}
	return out
}

// childRects divides r among k children along dir: equal shares, with
// the remainder going to the earliest children one cell each, so the
// results always tile r exactly. This is the single source of truth
// for split geometry — Layout and Split's refusal math (which
// validates via ComputeRects) both depend on it agreeing with itself.
func childRects(dir Dir, k int, r Rect) []Rect {
	out := make([]Rect, 0, k)
	if dir == SplitSideBySide {
		base, rem := r.W/k, r.W%k
		x := r.X
		for i := 0; i < k; i++ {
			w := base
			if i < rem {
				w++
			}
			out = append(out, Rect{X: x, Y: r.Y, W: w, H: r.H})
			x += w
		}
	} else {
		base, rem := r.H/k, r.H%k
		y := r.Y
		for i := 0; i < k; i++ {
			h := base
			if i < rem {
				h++
			}
			out = append(out, Rect{X: r.X, Y: y, W: r.W, H: h})
			y += h
		}
	}
	return out
}

// ComputeRects flattens Layout into a per-window rect map.
func (t *Tree) ComputeRects(bounds Rect) map[LeafID]Rect {
	out := make(map[LeafID]Rect)
	var walk func(LayoutNode)
	walk = func(n LayoutNode) {
		if n.Leaf {
			out[n.ID] = n.Rect
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(t.Layout(bounds))
	return out
}
