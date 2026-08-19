package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveNaming(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"project.tar.gz", "project"},
		{"notes.zip", "notes"},
		{"backup.tgz", "backup"},
		{"plain.txt", "plain"},
	} {
		if got := archiveBaseName(tc.in); got != tc.want {
			t.Errorf("archiveBaseName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if !IsArchive("x.TAR.GZ") {
		t.Error("IsArchive should ignore case")
	}
	if IsArchive("notes.txt") {
		t.Error("a text file is not an archive")
	}
	if got := archiveName("/tmp/work", []string{"a.txt", "b.txt"}); got != "work.zip" {
		t.Errorf("multi-item archive name = %q, want work.zip", got)
	}
}

// Round-trip through the real tools: zip writes it, ditto reads it back.
func TestArchiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "two.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "bundle.zip")
	msg := createArchive(dir, dst, []string{"one.txt", "sub"})().(archiveDoneMsg)
	if msg.err != nil {
		t.Fatalf("createArchive: %v (%s)", msg.err, msg.details)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("archive not written: %v", err)
	}

	out := filepath.Join(dir, "unpacked")
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	msg = extractArchive(dst, out)().(archiveDoneMsg)
	if msg.err != nil {
		t.Fatalf("extractArchive: %v (%s)", msg.err, msg.details)
	}
	got, err := os.ReadFile(filepath.Join(out, "sub", "two.txt"))
	if err != nil || string(got) != "world" {
		t.Errorf("extracted sub/two.txt = %q (%v), want %q", got, err, "world")
	}
}

func TestDirSizeCmd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), make([]byte, 2048), 0644); err != nil {
		t.Fatal(err)
	}
	msg := dirSizeCmd(dir)().(dirSizeResultMsg)
	if msg.Size != 2048 {
		t.Errorf("size = %d, want 2048", msg.Size)
	}
}

// Every extension the app can open must land on a handler kind, and the
// kinds must match what the settings screen offers a row for.
func TestHandlerKindMapping(t *testing.T) {
	for ext, want := range map[string]string{
		".png":  "image",
		".mp4":  "video",
		".mp3":  "audio",
		".pdf":  "pdf",
		".epub": "ebook",
		".zip":  "archive",
		".go":   "default",
		"":      "default",
	} {
		if got := handlerKindFor(ext); got != want {
			t.Errorf("handlerKindFor(%q) = %q, want %q", ext, got, want)
		}
	}
}
