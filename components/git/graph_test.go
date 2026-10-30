package git

import (
	"strings"
	"testing"
)

// rowString renders a graph row as plain text (glyph + spacer per lane),
// with trailing blanks removed.
func rowString(r graphRow) string {
	var b strings.Builder
	for _, c := range r.cells {
		b.WriteRune(c.glyph)
		b.WriteRune(c.spacer)
	}
	return strings.TrimRight(b.String(), " ")
}

func layoutStrings(commits []Commit, head string) []string {
	var out []string
	for _, r := range layoutGraph(commits, head) {
		out = append(out, rowString(r))
	}
	return out
}

func c(hash string, parents ...string) Commit { return Commit{Hash: hash, Parents: parents} }

func TestLayoutLinear(t *testing.T) {
	got := layoutStrings([]Commit{c("c", "b"), c("b", "a"), c("a")}, "c")
	want := []string{"◉", "●", "●"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

// main ← merge(m1, f2); f2 ← f1 ← base; m1 ← base.
func TestLayoutMergeAndFork(t *testing.T) {
	commits := []Commit{
		c("merge", "m1", "f2"),
		c("m1", "base"),
		c("f2", "f1"),
		c("f1", "base"),
		c("base"),
	}
	got := layoutStrings(commits, "merge")
	want := []string{
		"◉─╮",
		"● │",
		"│ ●",
		"│ ●",
		"●─╯",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A newer commit on another branch must not push HEAD off lane 0.
func TestLayoutHeadStaysLeft(t *testing.T) {
	commits := []Commit{
		c("other", "base"), // newest, not HEAD
		c("head", "base"),
		c("base"),
	}
	got := layoutStrings(commits, "head")
	want := []string{
		"│ ●",
		"◉ │",
		"●─╯",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
	rows := layoutGraph(commits, "head")
	if rows[1].lane != 0 {
		t.Fatalf("HEAD in lane %d", rows[1].lane)
	}
}

// Three lines (merge parent, side branch, mainline) converge on the root.
func TestLayoutThreeLinesConverge(t *testing.T) {
	commits := []Commit{
		c("m", "a", "x"), // lane 0, opens lane for x
		c("side", "s0"),  // new tip
		c("a", "root"),
		c("x", "root"),
		c("s0", "root"),
		c("root"),
	}
	for _, r := range layoutStrings(commits, "m") {
		if strings.ContainsAny(r, "?") {
			t.Fatalf("bad glyph in %q", r)
		}
	}
	rows := layoutGraph(commits, "m")
	last := rowString(rows[len(rows)-1])
	if !strings.HasPrefix(last, "●") || strings.Count(last, "╯") != 2 {
		t.Fatalf("root row = %q", last)
	}
}

func TestLogParentsAndRefs(t *testing.T) {
	dir := newRepo(t)
	sh(t, dir, "switch", "-q", "-c", "feature")
	write(t, dir, "b.txt", "b\n")
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-q", "-m", "feature")
	sh(t, dir, "switch", "-q", "main")
	write(t, dir, "c.txt", "c\n")
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-q", "-m", "main")
	sh(t, dir, "merge", "-q", "--no-edit", "feature")
	sh(t, dir, "tag", "v1")

	all, err := Log(dir, 50, true)
	if err != nil || len(all) != 4 {
		t.Fatalf("Log = %d commits, %v", len(all), err)
	}
	if !all[0].IsMerge() || all[0].Date == "" {
		t.Fatalf("top = %+v", all[0])
	}
	refs := strings.Join(all[0].Refs, ",")
	if !strings.Contains(refs, "HEAD -> main") || !strings.Contains(refs, "tag: v1") {
		t.Fatalf("refs = %q", refs)
	}

	// An unborn branch has an empty log, not an error.
	isolate(t)
	empty := t.TempDir()
	sh(t, empty, "init", "-q")
	if got, err := Log(empty, 10, true); err != nil || len(got) != 0 {
		t.Fatalf("unborn Log = %v, %v", got, err)
	}
}
