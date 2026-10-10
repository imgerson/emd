package diff

import (
	"strings"
	"testing"
)

func kinds(ops []Op) string {
	var b strings.Builder
	for _, o := range ops {
		b.WriteByte(o.Kind)
	}
	return b.String()
}

func TestLines(t *testing.T) {
	cases := []struct {
		name, a, b, want string
	}{
		{"same", "a\nb\n", "a\nb\n", "  "},
		{"append", "a\n", "a\nb\n", " +"},
		{"remove middle", "a\nb\nc", "a\nc", " - "},
		{"replace", "a\nb\nc", "a\nB\nc", " -+ "},
		{"from empty", "", "x\ny", "++"},
		{"to empty", "x", "", "-"},
		{"crlf equals lf", "a\r\nb\r\n", "a\nb\n", "  "},
	}
	for _, c := range cases {
		if got := kinds(Lines(c.a, c.b)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLinesKeepsEveryLine(t *testing.T) {
	a := "one\ntwo\nthree\nfour\nfive"
	b := "zero\none\nthree\nFOUR\nfive\nsix"
	var gotA, gotB []string
	for _, o := range Lines(a, b) {
		if o.Kind != '+' {
			gotA = append(gotA, o.Text)
		}
		if o.Kind != '-' {
			gotB = append(gotB, o.Text)
		}
	}
	if strings.Join(gotA, "\n") != a || strings.Join(gotB, "\n") != b {
		t.Fatalf("ops don't rebuild the inputs: %q / %q", gotA, gotB)
	}
}

func TestUnified(t *testing.T) {
	if got := Unified(Lines("a\nb", "a\nb"), "x", "y", 3); got != "" {
		t.Fatalf("no change should render nothing, got %q", got)
	}
	got := Unified(Lines("1\n2\n3\n4\n5\n6\n7\n8\n9\n10", "1\n2\n3\n4\nFIVE\n6\n7\n8\n9\n10"), "v1", "v2", 1)
	want := "--- v1\n+++ v2\n@@ -4,3 +4,3 @@\n 4\n-5\n+FIVE\n 6\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUnifiedSplitsDistantHunks(t *testing.T) {
	a := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj"
	b := "A\nb\nc\nd\ne\nf\ng\nh\ni\nJ"
	if n := strings.Count(Unified(Lines(a, b), "x", "y", 1), "@@ -"); n != 2 {
		t.Fatalf("want 2 hunks, got %d", n)
	}
}
