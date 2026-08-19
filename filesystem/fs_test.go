package filesystem

import (
	"testing"
	"time"
)

func TestSortOptions(t *testing.T) {
	t.Cleanup(func() { SetSortOptions(true, false) })

	entries := []FileEntry{
		{Name: "b.txt"}, {Name: "a.txt"}, {Name: "dir", IsDir: true},
	}

	SetSortOptions(true, false)
	SortEntries(entries, SortByName)
	if entries[0].Name != "dir" || entries[1].Name != "a.txt" {
		t.Errorf("dirs-first ascending gave %v", names(entries))
	}

	SetSortOptions(true, true)
	SortEntries(entries, SortByName)
	// Reversing flips the names but must not drop the directory to the
	// bottom — grouping and ordering are separate choices.
	if entries[0].Name != "dir" || entries[1].Name != "b.txt" {
		t.Errorf("dirs-first descending gave %v", names(entries))
	}

	SetSortOptions(false, false)
	SortEntries(entries, SortByName)
	if entries[0].Name != "a.txt" {
		t.Errorf("without dirs-first, plain name order expected, got %v", names(entries))
	}
}

func names(entries []FileEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func TestFormatSizeUnits(t *testing.T) {
	t.Cleanup(func() { SetFormatOptions("short", true) })

	SetFormatOptions("short", true)
	for _, tc := range []struct {
		size int64
		want string
	}{
		{512, "512 B"},
		{2048, "2.0 KiB"},
		{5 << 20, "5.0 MiB"},
	} {
		if got := FormatSize(tc.size); got != tc.want {
			t.Errorf("binary FormatSize(%d) = %q, want %q", tc.size, got, tc.want)
		}
	}

	SetFormatOptions("short", false)
	if got := FormatSize(2000); got != "2.0 kB" {
		t.Errorf("decimal FormatSize(2000) = %q, want 2.0 kB", got)
	}
	if got := FormatSize(999); got != "999 B" {
		t.Errorf("decimal FormatSize(999) = %q, want 999 B", got)
	}
}

func TestFormatTimeStyles(t *testing.T) {
	t.Cleanup(func() { SetFormatOptions("short", true) })
	when := time.Now().Add(-90 * time.Minute)

	SetFormatOptions("iso", true)
	if got := FormatTime(when); got != when.Format("2006-01-02 15:04") {
		t.Errorf("iso style gave %q", got)
	}

	SetFormatOptions("relative", true)
	if got := FormatTime(when); got != "1h ago" {
		t.Errorf("relative style gave %q, want 1h ago", got)
	}
}

// The file list gives the size column eight cells. A longer string does
// not get clipped there — it wraps into the next column — so the
// formatter itself has to stay inside the budget.
func TestFormatSizeFitsTheColumn(t *testing.T) {
	t.Cleanup(func() { SetFormatOptions("short", true) })

	sizes := []int64{0, 1, 999, 1023, 1024, 9_999, 940 * 1024, 1023 * 1024,
		5 << 20, 999 << 20, 12 << 30, 2 << 40}

	for _, binary := range []bool{true, false} {
		SetFormatOptions("short", binary)
		for _, size := range sizes {
			got := FormatSize(size)
			if len(got) > 8 {
				t.Errorf("FormatSize(%d) = %q (%d cells), want at most 8",
					size, got, len(got))
			}
		}
	}
}
