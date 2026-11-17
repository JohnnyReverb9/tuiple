package preview

import (
	"archive/zip"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnnyReverb9/tuiple/components/preview/mediarender"
	"github.com/JohnnyReverb9/tuiple/filesystem"
)

func TestKindOf(t *testing.T) {
	cases := []struct {
		entry filesystem.FileEntry
		head  string
		want  string
	}{
		{filesystem.FileEntry{Name: "main.go", Extension: ".go"}, "", "Go source"},
		{filesystem.FileEntry{Name: "Makefile"}, "", "Makefile"},
		{filesystem.FileEntry{Name: "deploy", IsExec: true}, "#!/usr/bin/env bash\necho", "bash script"},
		{filesystem.FileEntry{Name: "run", IsExec: true}, "#!/bin/sh\n", "sh script"},
		{filesystem.FileEntry{Name: "tuiple", IsExec: true}, "\xcf\xfa\xed\xfe....", "Mach-O executable"},
		{filesystem.FileEntry{Name: "tool", IsExec: true}, "", "Executable"},
		{filesystem.FileEntry{Name: "data.xyz", Extension: ".xyz"}, "", "XYZ file"},
		{filesystem.FileEntry{Name: "notes"}, "", "File"},
		{filesystem.FileEntry{Name: "src", IsDir: true}, "", "Folder"},
	}
	for _, c := range cases {
		if got := kindOf(c.entry, []byte(c.head)); got != c.want {
			t.Errorf("kindOf(%s) = %q, want %q", c.entry.Name, got, c.want)
		}
	}
}

func TestTextDetails(t *testing.T) {
	get := func(data string) map[string]string {
		out := map[string]string{}
		for _, d := range textDetails([]byte(data)) {
			out[d.Label] = d.Value
		}
		return out
	}
	if d := get("a\nb\nc\n"); d["Lines"] != "3" || d["Encoding"] != "UTF-8 · LF" {
		t.Errorf("LF file = %v", d)
	}
	if d := get("a\r\nb"); d["Lines"] != "2" || !strings.Contains(d["Encoding"], "CRLF") {
		t.Errorf("CRLF file = %v", d)
	}
	if d := get("\xef\xbb\xbfx\n"); !strings.HasPrefix(d["Encoding"], "UTF-8 with BOM") {
		t.Errorf("BOM file = %v", d)
	}
	if d := get(""); d["Lines"] != "empty" {
		t.Errorf("empty file = %v", d)
	}
}

func TestDirItemsAndZip(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644)
	if got := dirItems(dir); got != "3 · 1 folder, 2 files" {
		t.Errorf("dirItems = %q", got)
	}
	if got := dirItems(filepath.Join(dir, "sub")); got != "empty" {
		t.Errorf("empty dir = %q", got)
	}

	zpath := filepath.Join(dir, "x.zip")
	f, _ := os.Create(zpath)
	zw := zip.NewWriter(f)
	for _, name := range []string{"one.txt", "two/three.txt"} {
		w, _ := zw.Create(name)
		w.Write([]byte(strings.Repeat("z", 1000)))
	}
	zw.Close()
	f.Close()
	d := zipDetails(zpath)
	if len(d) != 1 || !strings.HasPrefix(d[0].Value, "2 files · ") {
		t.Errorf("zipDetails = %v", d)
	}
}

func TestImageDimensionsWithoutSpotlight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pic.png") // temp dirs are not indexed
	f, _ := os.Create(path)
	png.Encode(f, image.NewRGBA(image.Rect(0, 0, 64, 32)))
	f.Close()
	d := mediaDetails(path, mediarender.KindImage)
	if len(d) != 1 || d[0].Value != "64 × 32" {
		t.Errorf("mediaDetails = %v", d)
	}
}

func TestGitLabel(t *testing.T) {
	for code, want := range map[string]string{
		" M": "modified, not staged",
		"M ": "modified, staged",
		"MM": "modified (staged), then modified",
		"A ": "added, staged",
		"??": "untracked",
		"UU": "conflicted",
		"":   "",
	} {
		if got := gitLabel(code); got != want {
			t.Errorf("gitLabel(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestInfoRowsOrderAndLayout(t *testing.T) {
	info := FileInfo{
		Name: "main.go", Kind: "Go source", Size: 2048,
		ModTime: "Nov 18 00:12", Created: "Nov 1 09:00", Perms: "-rw-r--r--",
		Details: []Detail{{"Lines", "120"}},
	}
	var labels []string
	for _, r := range infoRows(info, " M") {
		labels = append(labels, r.Label)
	}
	if got := strings.Join(labels, ","); got != "Kind,Size,Lines,Modified,Created,Perms,Git" {
		t.Fatalf("rows = %s", got)
	}
	lines := renderInfoRows(infoRows(info, ""), 60)
	// Values start in the same column on every row.
	col := strings.Index(lines[0], "Go source")
	if col < 0 || strings.Index(lines[1], "2.0") != col {
		t.Fatalf("values not aligned:\n%s", strings.Join(lines, "\n"))
	}
}
