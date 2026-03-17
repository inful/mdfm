package mdfm

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestParseWithoutFrontmatter(t *testing.T) {
	t.Parallel()

	input := []byte("# Title\n\nBody\n")
	doc, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if doc.HasFrontmatter() {
		t.Fatalf("expected no frontmatter")
	}

	if !bytes.Equal(doc.Body(), input) {
		t.Fatalf("unexpected body: %q", string(doc.Body()))
	}
}

func TestParseWithFrontmatterAndBody(t *testing.T) {
	t.Parallel()

	input := []byte("---\ntitle: hello\ncount: 3\n---\n# Heading\n")
	doc, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if !doc.HasFrontmatter() {
		t.Fatalf("expected frontmatter")
	}

	title, ok, err := doc.Get("title")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected title key")
	}
	if title != "hello" {
		t.Fatalf("unexpected title: %#v", title)
	}

	count, ok, err := doc.Get("count")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected count key")
	}
	if count != 3 {
		t.Fatalf("unexpected count: %#v", count)
	}

	if string(doc.Body()) != "# Heading\n" {
		t.Fatalf("unexpected body: %q", string(doc.Body()))
	}
}

func TestParseCRLF(t *testing.T) {
	t.Parallel()

	input := []byte("---\r\ntitle: hello\r\n---\r\nbody\r\n")
	doc, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	output, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}

	if !bytes.Equal(output, input) {
		t.Fatalf("roundtrip mismatch\nwant: %q\n got: %q", string(input), string(output))
	}
}

func TestParseUnclosedFrontmatter(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("---\ntitle: x\nbody\n"))
	if !errors.Is(err, ErrUnclosedFrontmatter) {
		t.Fatalf("expected ErrUnclosedFrontmatter, got: %v", err)
	}
}

func TestParseNonMappingFrontmatter(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("---\n- item\n---\nbody\n"))
	if !errors.Is(err, ErrFrontmatterNotMapping) {
		t.Fatalf("expected ErrFrontmatterNotMapping, got: %v", err)
	}
}

func TestSetGetDeleteKeys(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("content\n"))
	mustSet(t, doc, "title", "Hello")
	mustSet(t, doc, "tags", []string{"go", "markdown"})

	keys := mustKeys(t, doc)
	if !slices.Equal(keys, []string{"title", "tags"}) {
		t.Fatalf("unexpected keys: %#v", keys)
	}

	value, ok := mustGet(t, doc, "title")
	if !ok || value != "Hello" {
		t.Fatalf("unexpected value: %#v (ok=%v)", value, ok)
	}

	deleted := mustDelete(t, doc, "title")
	if !deleted {
		t.Fatalf("expected key to be deleted")
	}

	_, ok = mustGet(t, doc, "title")
	if ok {
		t.Fatalf("expected deleted key to be missing")
	}
}

func TestSetFrontmatterAndBytes(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if err = doc.SetFrontmatter(map[string]any{"a": 1, "b": true}); err != nil {
		t.Fatalf("SetFrontmatter returned error: %v", err)
	}

	output, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}

	if string(output) != "---\na: 1\nb: true\n---\nbody\n" {
		t.Fatalf("unexpected output: %q", string(output))
	}
}

func TestUpdateFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")

	if err := os.WriteFile(path, []byte("---\ntitle: old\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	err := UpdateFile(path, func(doc *Document) error {
		return doc.Set("title", "new")
	})
	if err != nil {
		t.Fatalf("UpdateFile returned error: %v", err)
	}

	updated, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if string(updated) != "---\ntitle: new\n---\nbody\n" {
		t.Fatalf("unexpected updated content: %q", string(updated))
	}
}

func TestReadFileRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions can vary on Windows runners")
	}

	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	link := filepath.Join(dir, "link.md")

	if err := os.WriteFile(target, []byte("# test\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	_, err := ReadFile(link)
	if err == nil {
		t.Fatalf("expected error when reading symlink")
	}
}

func TestEmptyKeyValidation(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if err = doc.Set("", "x"); !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey from Set, got: %v", err)
	}

	_, _, err = doc.Get("")
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey from Get, got: %v", err)
	}

	_, err = doc.Delete("")
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey from Delete, got: %v", err)
	}
}

func mustParse(t *testing.T, input []byte) *Document {
	t.Helper()

	doc, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	return doc
}

func mustSet(t *testing.T, doc *Document, key string, value any) {
	t.Helper()

	if err := doc.Set(key, value); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
}

func mustKeys(t *testing.T, doc *Document) []string {
	t.Helper()

	keys, err := doc.Keys()
	if err != nil {
		t.Fatalf("Keys returned error: %v", err)
	}

	return keys
}

func mustGet(t *testing.T, doc *Document, key string) (any, bool) {
	t.Helper()

	value, ok, err := doc.Get(key)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	return value, ok
}

func mustDelete(t *testing.T, doc *Document, key string) bool {
	t.Helper()

	deleted, err := doc.Delete(key)
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	return deleted
}
