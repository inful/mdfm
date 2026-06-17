package mdfm

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	emptyFrontmatterDoc = "---\n---\nbody\n"
	windowsOS           = "windows"
)

// This file exercises two concerns:
//
//  1. Nil-receiver safety for every public method on *Document. Each public
//     accessor and mutator must tolerate a nil *Document without panicking.
//  2. Untested branches on the public surface (empty frontmatter blocks,
//     non-mapping frontmatter, file errors, Mutate error paths).
//
// The tests use the unexported `Document` fields when needed to construct
// pathological inputs (e.g., a Document whose frontmatter node is a sequence).
// They are co-located with the package so this access is permitted.

// --- Nil-receiver safety -------------------------------------------------

func TestNilReceiverHasFrontmatter(t *testing.T) {
	t.Parallel()

	var doc *Document
	if got := doc.HasFrontmatter(); got {
		t.Fatalf("expected false for nil receiver, got %v", got)
	}
}

func TestNilReceiverBody(t *testing.T) {
	t.Parallel()

	var doc *Document
	if got := doc.Body(); got != nil {
		t.Fatalf("expected nil for nil receiver, got %q", got)
	}
}

func TestNilReceiverSetBody(t *testing.T) {
	t.Parallel()

	var doc *Document
	doc.SetBody([]byte("hello")) // must not panic
}

func TestNilReceiverGet(t *testing.T) {
	t.Parallel()

	var doc *Document
	value, ok, err := doc.Get("title")
	if err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for nil receiver")
	}
	if value != nil {
		t.Fatalf("expected nil value for nil receiver, got %v", value)
	}
}

func TestNilReceiverHas(t *testing.T) {
	t.Parallel()

	var doc *Document
	has, err := doc.Has("title")
	if err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
	if has {
		t.Fatalf("expected has=false for nil receiver")
	}
}

func TestNilReceiverGetString(t *testing.T) {
	t.Parallel()

	var doc *Document
	value, ok, err := doc.GetString("title")
	if err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for nil receiver")
	}
	if value != "" {
		t.Fatalf("expected empty string for nil receiver, got %q", value)
	}
}

func TestNilReceiverSetString(t *testing.T) {
	t.Parallel()

	var doc *Document
	if err := doc.SetString("title", "hello"); err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
}

func TestNilReceiverSet(t *testing.T) {
	t.Parallel()

	var doc *Document
	if err := doc.Set("title", "hello"); err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
}

func TestNilReceiverDelete(t *testing.T) {
	t.Parallel()

	var doc *Document
	deleted, err := doc.Delete("title")
	if err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
	if deleted {
		t.Fatalf("expected deleted=false for nil receiver")
	}
}

func TestNilReceiverKeys(t *testing.T) {
	t.Parallel()

	var doc *Document
	keys, err := doc.Keys()
	if err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
	if keys != nil {
		t.Fatalf("expected nil keys for nil receiver, got %v", keys)
	}
}

func TestNilReceiverBytes(t *testing.T) {
	t.Parallel()

	var doc *Document
	b, err := doc.Bytes()
	if err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
	if b != nil {
		t.Fatalf("expected nil bytes for nil receiver, got %q", b)
	}
}

func TestNilReceiverWriteFile(t *testing.T) {
	t.Parallel()

	var doc *Document
	if err := doc.WriteFile(filepath.Join(t.TempDir(), "nil.md"), 0o600); err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
}

func TestNilReceiverFrontmatter(t *testing.T) {
	t.Parallel()

	var doc *Document
	fm, err := doc.Frontmatter()
	if err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
	if len(fm) != 0 {
		t.Fatalf("expected empty map for nil receiver, got %v", fm)
	}
}

func TestNilReceiverSetFrontmatter(t *testing.T) {
	t.Parallel()

	var doc *Document
	if err := doc.SetFrontmatter(map[string]any{"a": 1}); err != nil {
		t.Fatalf("expected nil error for nil receiver, got %v", err)
	}
}

// --- Parse ----------------------------------------------------------------

func TestParseEmptyFrontmatterBlock(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte(emptyFrontmatterDoc))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !doc.HasFrontmatter() {
		t.Fatalf("expected frontmatter to be present")
	}
	if string(doc.Body()) != "body\n" {
		t.Fatalf("unexpected body: %q", string(doc.Body()))
	}

	keys, err := doc.Keys()
	if err != nil {
		t.Fatalf("Keys returned error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected no keys in empty frontmatter, got %v", keys)
	}

	output, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if string(output) != emptyFrontmatterDoc {
		t.Fatalf("unexpected output: %q", string(output))
	}
}

// --- Mutate / MutateString -----------------------------------------------

func TestMutateNilCallback(t *testing.T) {
	t.Parallel()

	content := []byte("---\ntitle: old\n---\nbody\n")
	updated, changed, err := Mutate(content, nil)
	if err != nil {
		t.Fatalf("Mutate returned error: %v", err)
	}
	if changed {
		t.Fatalf("expected no change with nil callback")
	}
	if !bytes.Equal(updated, content) {
		t.Fatalf("expected bytes to be unchanged, got %q", string(updated))
	}
}

func TestMutateMutationError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	_, _, err := Mutate([]byte("---\ntitle: x\n---\nbody\n"), func(*Document) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom error, got %v", err)
	}
}

func TestMutateParseError(t *testing.T) {
	t.Parallel()

	_, _, err := Mutate([]byte("---\ntitle: [oops\n---\nbody\n"), nil)
	if err == nil || !strings.Contains(err.Error(), "failed to parse markdown") {
		t.Fatalf("expected wrapped parse error, got %v", err)
	}
}

func TestMutateStringParseError(t *testing.T) {
	t.Parallel()

	_, _, err := MutateString("---\ntitle: [oops\n---\nbody\n", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to parse markdown") {
		t.Fatalf("expected wrapped parse error, got %v", err)
	}
}

// --- ReadFile / UpdateFile -----------------------------------------------

func TestReadFileNotFound(t *testing.T) {
	t.Parallel()

	_, err := ReadFile(filepath.Join(t.TempDir(), "does-not-exist.md"))
	if err == nil {
		t.Fatalf("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "failed to stat file") {
		t.Fatalf("expected stat error, got %v", err)
	}
}

// TestReadFileIsDirectory covers the os.ReadFile error path: the path exists
// and is not a symlink, but cannot be read as a regular file.
func TestReadFileIsDirectory(t *testing.T) {
	t.Parallel()

	_, err := ReadFile(t.TempDir())
	if err == nil {
		t.Fatalf("expected error for directory path")
	}
	if !strings.Contains(err.Error(), "failed to read file") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestUpdateFileRefusesSymlink(t *testing.T) {
	if runtime.GOOS == windowsOS {
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

	err := UpdateFile(link, func(*Document) error { return nil })
	if err == nil {
		t.Fatalf("expected error when updating symlink")
	}
	if !strings.Contains(err.Error(), "refusing to update symlink") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}
}

// --- Frontmatter / SetFrontmatter ----------------------------------------

func TestFrontmatterNoFrontmatter(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	fm, err := doc.Frontmatter()
	if err != nil {
		t.Fatalf("Frontmatter returned error: %v", err)
	}
	if fm == nil || len(fm) != 0 {
		t.Fatalf("expected empty map for no frontmatter, got %#v", fm)
	}
}

func TestFrontmatterEmptyFrontmatter(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte(emptyFrontmatterDoc))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	fm, err := doc.Frontmatter()
	if err != nil {
		t.Fatalf("Frontmatter returned error: %v", err)
	}
	if fm == nil || len(fm) != 0 {
		t.Fatalf("expected empty map for empty frontmatter, got %#v", fm)
	}
}

func TestSetFrontmatterEmptyMap(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if setErr := doc.SetFrontmatter(map[string]any{}); setErr != nil {
		t.Fatalf("SetFrontmatter returned error: %v", setErr)
	}
	if !doc.HasFrontmatter() {
		t.Fatalf("expected frontmatter to be set")
	}

	output, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if string(output) != emptyFrontmatterDoc {
		t.Fatalf("unexpected output: %q", string(output))
	}
}

func TestSetFrontmatterNilMap(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	// A nil map is a valid empty map[string]any; SetFrontmatter treats it as
	// replacing the frontmatter with an empty mapping.
	if setErr := doc.SetFrontmatter(nil); setErr != nil {
		t.Fatalf("SetFrontmatter(nil) returned error: %v", setErr)
	}
	fm, err := doc.Frontmatter()
	if err != nil {
		t.Fatalf("Frontmatter returned error: %v", err)
	}
	if len(fm) != 0 {
		t.Fatalf("expected empty map after SetFrontmatter(nil), got %#v", fm)
	}
}

// TestSetFrontmatterSeedsNewline covers the d.newline == "" branch which is
// only reachable on a Document that has not gone through Parse, since Parse
// always seeds a newline.
func TestSetFrontmatterSeedsNewline(t *testing.T) {
	t.Parallel()

	doc := &Document{}
	if err := doc.SetFrontmatter(map[string]any{"a": 1}); err != nil {
		t.Fatalf("SetFrontmatter returned error: %v", err)
	}
	if doc.newline != lf {
		t.Fatalf("expected newline to be seeded to lf, got %q", doc.newline)
	}
}

// --- Get / Has / GetString / Delete / Keys -------------------------------

// TestSetUnencodableValue asserts that Set returns a wrapped error for values
// that yaml.v3 cannot encode (function, channel, unsafe pointer) rather than
// propagating a panic from the third-party library. The deferred recover is
// what would fire today if the production code did not handle this; once the
// production fix lands the recover is a no-op.
func TestSetUnencodableValue(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Set panicked on unencodable value: %v", r)
		}
	}()

	err := doc.Set("fn", func() {})
	if err == nil {
		t.Fatalf("expected error for function value")
	}
	if !strings.Contains(err.Error(), "failed to encode value") {
		t.Fatalf("expected encode error, got %v", err)
	}
}

// TestSetFrontmatterUnencodableValue asserts the same contract for
// SetFrontmatter when a map value contains an unencodable type.
func TestSetFrontmatterUnencodableValue(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SetFrontmatter panicked on unencodable value: %v", r)
		}
	}()

	err := doc.SetFrontmatter(map[string]any{"fn": func() {}})
	if err == nil {
		t.Fatalf("expected error for unencodable value in map")
	}
	if !strings.Contains(err.Error(), "failed to encode") {
		t.Fatalf("expected encode error, got %v", err)
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\ntitle: hello\n---\nbody\n"))
	value, ok, err := doc.Get("missing")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for missing key")
	}
	if value != nil {
		t.Fatalf("expected nil value for missing key, got %v", value)
	}
}

func TestGetNoFrontmatter(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	value, ok, err := doc.Get("title")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for no frontmatter")
	}
	if value != nil {
		t.Fatalf("expected nil value for no frontmatter")
	}
}

func TestGetNonMappingFrontmatter(t *testing.T) {
	t.Parallel()

	doc := &Document{hasFrontmatter: true, frontmatter: yaml.Node{Kind: yaml.SequenceNode}}
	_, _, err := doc.Get("title")
	if !errors.Is(err, ErrFrontmatterNotMapping) {
		t.Fatalf("expected ErrFrontmatterNotMapping, got %v", err)
	}
}

func TestHasNoFrontmatter(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	has, err := doc.Has("title")
	if err != nil {
		t.Fatalf("Has returned error: %v", err)
	}
	if has {
		t.Fatalf("expected has=false for no frontmatter")
	}
}

func TestHasNonMappingFrontmatter(t *testing.T) {
	t.Parallel()

	doc := &Document{hasFrontmatter: true, frontmatter: yaml.Node{Kind: yaml.SequenceNode}}
	_, err := doc.Has("title")
	if !errors.Is(err, ErrFrontmatterNotMapping) {
		t.Fatalf("expected ErrFrontmatterNotMapping, got %v", err)
	}
}

func TestGetStringNoFrontmatter(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	value, ok, err := doc.GetString("title")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for no frontmatter")
	}
	if value != "" {
		t.Fatalf("expected empty string for no frontmatter, got %q", value)
	}
}

func TestDeleteNoFrontmatter(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	deleted, err := doc.Delete("title")
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if deleted {
		t.Fatalf("expected deleted=false for no frontmatter")
	}
}

func TestDeleteNonMappingFrontmatter(t *testing.T) {
	t.Parallel()

	doc := &Document{hasFrontmatter: true, frontmatter: yaml.Node{Kind: yaml.SequenceNode}}
	_, err := doc.Delete("title")
	if !errors.Is(err, ErrFrontmatterNotMapping) {
		t.Fatalf("expected ErrFrontmatterNotMapping, got %v", err)
	}
}

func TestKeysNoFrontmatter(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte("body\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	keys, err := doc.Keys()
	if err != nil {
		t.Fatalf("Keys returned error: %v", err)
	}
	if keys != nil {
		t.Fatalf("expected nil keys for no frontmatter, got %v", keys)
	}
}

func TestKeysNonMappingFrontmatter(t *testing.T) {
	t.Parallel()

	doc := &Document{hasFrontmatter: true, frontmatter: yaml.Node{Kind: yaml.SequenceNode}}
	_, err := doc.Keys()
	if !errors.Is(err, ErrFrontmatterNotMapping) {
		t.Fatalf("expected ErrFrontmatterNotMapping, got %v", err)
	}
}

// TestParseDuplicateKeyInSequence covers validateUniqueKeysInChildren, which
// walks sequence children and recurses into their frontmatter mapping checks.
// A duplicate key inside a sequence-of-mappings should still be rejected.
func TestParseDuplicateKeyInSequence(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("---\nlist:\n  - a: 1\n    a: 2\n---\nbody\n"))
	if !errors.Is(err, ErrDuplicateFrontmatterKey) {
		t.Fatalf("expected ErrDuplicateFrontmatterKey, got %v", err)
	}
}

// TestSetNilValue covers the nodeFromValue path for a nil value. The document
// must accept nil and round-trip the value back to nil on Get.
func TestSetNilValue(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))
	if err := doc.Set("nullkey", nil); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	value, ok, err := doc.Get("nullkey")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected nullkey to exist")
	}
	if value != nil {
		t.Fatalf("expected nil value, got %#v", value)
	}
}

// --- Bytes / WriteFile ----------------------------------------------------

func TestBytesEmptyFrontmatter(t *testing.T) {
	t.Parallel()

	doc, err := Parse([]byte(emptyFrontmatterDoc))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	output, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if string(output) != emptyFrontmatterDoc {
		t.Fatalf("unexpected output: %q", string(output))
	}
}

func TestBytesNonMappingFrontmatter(t *testing.T) {
	t.Parallel()

	doc := &Document{
		hasFrontmatter: true,
		frontmatter: yaml.Node{
			Kind: yaml.SequenceNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: yamlStringTag, Value: "item"},
			},
		},
	}
	_, err := doc.Bytes()
	if !errors.Is(err, ErrFrontmatterNotMapping) {
		t.Fatalf("expected ErrFrontmatterNotMapping, got %v", err)
	}
}

// TestBytesSeedsNewline covers the `if newline == ""` branch in Bytes that
// fires when a Document has frontmatter but no observed body (so Parse never
// ran). This can only happen for a manually-constructed Document.
func TestBytesSeedsNewline(t *testing.T) {
	t.Parallel()

	doc := &Document{
		hasFrontmatter: true,
		frontmatter: yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: yamlStringTag, Value: "a"},
				{Kind: yaml.ScalarNode, Tag: yamlStringTag, Value: "one"},
			},
		},
	}
	output, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if string(output) != "---\na: one\n---\n" {
		t.Fatalf("unexpected output: %q", string(output))
	}
}

func TestWriteFileCreatesNew(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "new.md")
	doc := mustParse(t, []byte("---\ntitle: hello\n---\nbody\n"))
	if err := doc.WriteFile(path, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	content, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "---\ntitle: hello\n---\nbody\n" {
		t.Fatalf("unexpected content: %q", string(content))
	}
}

// TestWriteFileBytesError covers the Bytes error path. A manually-constructed
// Document with a non-mapping frontmatter that has content cannot be
// serialized.
func TestWriteFileBytesError(t *testing.T) {
	t.Parallel()

	doc := &Document{
		hasFrontmatter: true,
		frontmatter: yaml.Node{
			Kind: yaml.SequenceNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: yamlStringTag, Value: "item"},
			},
		},
	}
	err := doc.WriteFile(filepath.Join(t.TempDir(), "out.md"), 0o600)
	if err == nil || !strings.Contains(err.Error(), "failed to serialize document") {
		t.Fatalf("expected serialize error, got %v", err)
	}
}

// TestWriteFileLstatError covers the `else if !errors.Is(err, os.ErrNotExist)`
// branch in WriteFile. A NUL byte in the path causes os.Lstat to fail with
// an "invalid argument" error (not ErrNotExist) on POSIX systems.
func TestWriteFileLstatError(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))
	err := doc.WriteFile("\x00", 0o600)
	if err == nil {
		t.Skip("NUL byte path did not error on this platform")
	}
	if !strings.Contains(err.Error(), "failed to stat file") {
		t.Fatalf("expected stat error, got %v", err)
	}
}
