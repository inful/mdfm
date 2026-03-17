package mdfm

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

func TestParseMalformedYAMLFrontmatter(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("---\ntitle: [oops\n---\nbody\n"))
	if err == nil {
		t.Fatalf("expected parse error for malformed YAML")
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

	has, err := doc.Has("tags")
	if err != nil {
		t.Fatalf("Has returned error: %v", err)
	}
	if !has {
		t.Fatalf("expected tags key to exist")
	}

	has, err = doc.Has("title")
	if err != nil {
		t.Fatalf("Has returned error: %v", err)
	}
	if has {
		t.Fatalf("expected title key to be missing")
	}
}

func TestHasValidationAndNoFrontmatter(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))

	has, err := doc.Has("missing")
	if err != nil {
		t.Fatalf("Has returned error: %v", err)
	}
	if has {
		t.Fatalf("expected missing key to not exist")
	}

	_, err = doc.Has("")
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey from Has, got: %v", err)
	}
}

func TestSetExistingKeyIsIdempotent(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\ntitle: one\n---\nbody\n"))
	mustSet(t, doc, "title", "two")

	first := mustBytes(t, doc)

	mustSet(t, doc, "title", "two")
	second := mustBytes(t, doc)

	if !bytes.Equal(first, second) {
		t.Fatalf("expected idempotent bytes after repeated Set")
	}
}

func TestDeleteMissingKeyIsIdempotent(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\ntitle: one\n---\nbody\n"))

	deleted := mustDelete(t, doc, "missing")
	if deleted {
		t.Fatalf("expected first delete of missing key to return false")
	}

	first := mustBytes(t, doc)

	deleted = mustDelete(t, doc, "missing")
	if deleted {
		t.Fatalf("expected second delete of missing key to return false")
	}

	second := mustBytes(t, doc)
	if !bytes.Equal(first, second) {
		t.Fatalf("expected idempotent bytes after repeated Delete")
	}
}

func TestSetExistingKeyPreservesKeyOrder(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\na: 1\nb: 2\n---\nbody\n"))
	mustSet(t, doc, "a", 10)

	keys := mustKeys(t, doc)
	if !slices.Equal(keys, []string{"a", "b"}) {
		t.Fatalf("unexpected key order after update: %#v", keys)
	}
}

func TestSetMissingKeyAppendsInDocumentOrder(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\na: 1\nb: 2\n---\nbody\n"))
	mustSet(t, doc, "c", 3)

	keys := mustKeys(t, doc)
	if !slices.Equal(keys, []string{"a", "b", "c"}) {
		t.Fatalf("unexpected key order after append: %#v", keys)
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

func TestSetPreservesCRLFWhenFrontmatterExists(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\r\ntitle: old\r\n---\r\nbody\r\n"))
	mustSet(t, doc, "title", "new")

	output := mustBytes(t, doc)
	if !bytes.Contains(output, []byte("\r\n")) {
		t.Fatalf("expected CRLF newlines in output")
	}
	lfOnly := bytes.ReplaceAll(output, []byte("\r\n"), nil)
	if bytes.Contains(lfOnly, []byte("\n")) {
		t.Fatalf("expected no LF-only newlines")
	}
}

func TestSetCreatesFrontmatterWithPreferredNewline(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\r\n"))
	mustSet(t, doc, "title", "new")

	output := mustBytes(t, doc)
	if !bytes.Equal(output, []byte("---\r\ntitle: new\r\n---\r\nbody\r\n")) {
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

	_, _, err = doc.GetString("")
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey from GetString, got: %v", err)
	}

	if err = doc.SetString("", "x"); !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey from SetString, got: %v", err)
	}
}

func TestGetSetStringHelpers(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))
	if err := doc.SetString("uid", "abc-123"); err != nil {
		t.Fatalf("SetString returned error: %v", err)
	}

	uid, ok, err := doc.GetString("uid")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected uid key")
	}
	if uid != "abc-123" {
		t.Fatalf("unexpected uid: %q", uid)
	}

	missing, ok, err := doc.GetString("missing")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected missing key to not exist")
	}
	if missing != "" {
		t.Fatalf("expected empty string for missing key")
	}
}

func TestGetStringTypeMismatch(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))
	mustSet(t, doc, "count", 1)

	_, ok, err := doc.GetString("count")
	if err == nil {
		t.Fatalf("expected type mismatch error")
	}
	if !ok {
		t.Fatalf("expected key to be reported as present on type mismatch")
	}
}

func TestMutateContentHelpers(t *testing.T) {
	t.Parallel()

	content := []byte("---\ntitle: old\n---\nbody\n")
	updated, changed, err := Mutate(content, func(doc *Document) error {
		return doc.SetString("title", "new")
	})
	if err != nil {
		t.Fatalf("Mutate returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected content to change")
	}

	updatedDoc := mustParse(t, updated)
	title, ok, err := updatedDoc.GetString("title")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if !ok || title != "new" {
		t.Fatalf("unexpected title: %q (ok=%v)", title, ok)
	}

	second, changed, err := Mutate(updated, func(doc *Document) error {
		return doc.SetString("title", "new")
	})
	if err != nil {
		t.Fatalf("Mutate returned error: %v", err)
	}
	if changed {
		t.Fatalf("expected no-op mutation to report changed=false")
	}
	if !bytes.Equal(second, updated) {
		t.Fatalf("expected no-op mutation to preserve bytes")
	}
}

func TestMutateStringHelper(t *testing.T) {
	t.Parallel()

	content := "---\r\ntitle: old\r\n---\r\nbody\r\n"
	updated, changed, err := MutateString(content, func(doc *Document) error {
		return doc.SetString("title", "new")
	})
	if err != nil {
		t.Fatalf("MutateString returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected content to change")
	}
	if !strings.Contains(updated, "\r\n") {
		t.Fatalf("expected CRLF to be preserved")
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

func mustBytes(t *testing.T, doc *Document) []byte {
	t.Helper()

	b, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}

	return b
}
