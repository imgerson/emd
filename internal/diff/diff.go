// Package diff renders a unified line diff between two texts.
package diff

import (
	"fmt"
	"strings"
)

// Op is one line of a diff: ' ' kept, '-' removed, '+' added.
type Op struct {
	Kind byte
	Text string
}

// maxCells caps the LCS table. ponytail: past it the diff is "all removed, all added";
// switch to Myers if real documents hit it.
const maxCells = 4_000_000

// Lines diffs a and b line by line (LCS). CRLF is treated as LF.
func Lines(a, b string) []Op {
	x, y := split(a), split(b)
	// Trim the common prefix and suffix: cheap, and keeps the table small for typical edits.
	pre := 0
	for pre < len(x) && pre < len(y) && x[pre] == y[pre] {
		pre++
	}
	suf := 0
	for suf < len(x)-pre && suf < len(y)-pre && x[len(x)-1-suf] == y[len(y)-1-suf] {
		suf++
	}
	mx, my := x[pre:len(x)-suf], y[pre:len(y)-suf]

	var ops []Op
	for _, l := range x[:pre] {
		ops = append(ops, Op{' ', l})
	}
	ops = append(ops, middle(mx, my)...)
	for _, l := range x[len(x)-suf:] {
		ops = append(ops, Op{' ', l})
	}
	return ops
}

func middle(x, y []string) []Op {
	var ops []Op
	if len(x)*len(y) > maxCells {
		for _, l := range x {
			ops = append(ops, Op{'-', l})
		}
		for _, l := range y {
			ops = append(ops, Op{'+', l})
		}
		return ops
	}
	// lcs[i][j] = length of the LCS of x[i:] and y[j:]
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		switch {
		case x[i] == y[j]:
			ops = append(ops, Op{' ', x[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, Op{'-', x[i]})
			i++
		default:
			ops = append(ops, Op{'+', y[j]})
			j++
		}
	}
	for ; i < len(x); i++ {
		ops = append(ops, Op{'-', x[i]})
	}
	for ; j < len(y); j++ {
		ops = append(ops, Op{'+', y[j]})
	}
	return ops
}

func split(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// Unified renders ops as a unified diff with `context` unchanged lines around each change.
// It returns "" when nothing changed.
func Unified(ops []Op, fromName, toName string, context int) string {
	changed := false
	for _, o := range ops {
		if o.Kind != ' ' {
			changed = true
			break
		}
	}
	if !changed {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", fromName, toName)

	// Positions (1-based line numbers in a and b) before each op.
	type pos struct{ a, b int }
	at := make([]pos, len(ops)+1)
	at[0] = pos{1, 1}
	for k, o := range ops {
		at[k+1] = at[k]
		if o.Kind != '+' {
			at[k+1].a++
		}
		if o.Kind != '-' {
			at[k+1].b++
		}
	}

	for k := 0; k < len(ops); {
		if ops[k].Kind == ' ' {
			k++
			continue
		}
		start := max(0, k-context)
		end := k
		// Extend the hunk while changes are within 2*context of each other.
		for end < len(ops) {
			if ops[end].Kind != ' ' {
				end++
				continue
			}
			next := end
			for next < len(ops) && ops[next].Kind == ' ' {
				next++
			}
			if next < len(ops) && next-end <= 2*context {
				end = next
				continue
			}
			end = min(len(ops), end+context)
			break
		}
		na, nb := 0, 0
		for _, o := range ops[start:end] {
			if o.Kind != '+' {
				na++
			}
			if o.Kind != '-' {
				nb++
			}
		}
		sa, sb := at[start].a, at[start].b
		if na == 0 { // GNU diff/patch: an empty side starts at the line before
			sa--
		}
		if nb == 0 {
			sb--
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", sa, na, sb, nb)
		for _, o := range ops[start:end] {
			b.WriteByte(o.Kind)
			b.WriteString(o.Text)
			b.WriteByte('\n')
		}
		k = end
	}
	return b.String()
}
