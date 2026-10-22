package mdfm

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	testValueNew   = "new"
	testValueHello = "hello"
	testKeyTitle   = "title"
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

	title, ok, err := doc.Get(testKeyTitle)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected title key")
	}
	if title != testValueHello {
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

// TestParseWithoutTrailingNewline covers the final return in scanLine, where
// the last line of a file has no newline. Every other fixture ends with "\n",
// so without this the suite passes even if that return is broken -- and a
// broken one makes every such file fail with ErrUnclosedFrontmatter.
func TestParseWithoutTrailingNewline(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		input string
		body  string
		key   string
	}{
		"closing marker":         {"---\ntitle: a\n---", "", testKeyTitle},
		"body":                   {"---\ntitle: a\n---\nbody no nl", "body no nl", testKeyTitle},
		"alt closing marker":     {"---\ntitle: a\n...", "", testKeyTitle},
		"no frontmatter":         {"plain body no newline", "plain body no newline", ""},
		"single line":            {"x", "x", ""},
		"frontmatter only, crlf": {"---\r\ntitle: a\r\n---\r\nb", "b", testKeyTitle},
	} {
		assertParsesWithoutTrailingNewline(t, name, tc.input, tc.body, tc.key)
	}
}

// assertParsesWithoutTrailingNewline checks that a file whose last line has no
// newline parses, keeps its body, and stays parseable after a rewrite (Bytes
// adds the missing newline the file did not have).
func assertParsesWithoutTrailingNewline(t *testing.T, name, input, wantBody, wantKey string) {
	t.Helper()

	doc, err := ParseString(input)
	if err != nil {
		t.Fatalf("%s: ParseString returned error: %v", name, err)
	}
	if got := string(doc.Body()); got != wantBody {
		t.Fatalf("%s: body = %q, want %q", name, got, wantBody)
	}

	if wantKey == "" {
		if doc.HasFrontmatter() {
			t.Fatalf("%s: expected no frontmatter", name)
		}
		return
	}

	if !doc.HasFrontmatter() {
		t.Fatalf("%s: expected frontmatter", name)
	}
	if _, ok, getErr := doc.Get(wantKey); getErr != nil || !ok {
		t.Fatalf("%s: Get(%q) ok=%v err=%v", name, wantKey, ok, getErr)
	}

	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("%s: Bytes returned error: %v", name, err)
	}
	if _, err = ParseString(string(out)); err != nil {
		t.Fatalf("%s: rewrite does not re-parse: %v", name, err)
	}
}

// TestDelimiterAllowsTrailingWhitespace pins the rule in isMarker's comment:
// trailing whitespace after a delimiter is allowed. Editors and formatters
// produce "--- " and "---\t" routinely, and without the trim those files stop
// being frontmatter at all.
func TestDelimiterAllowsTrailingWhitespace(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"opener tab":   "---\t\ntitle: a\n---\nbody\n",
		"opener space": "--- \ntitle: a\n---\nbody\n",
		"closer tab":   "---\ntitle: a\n---\t\nbody\n",
		"closer space": "---\ntitle: a\n--- \nbody\n",
		"both":         "--- \ntitle: a\n---\t\nbody\n",
		"crlf opener":  "---\r\ntitle: a\r\n---\r\nbody\r\n",
	} {
		doc, err := ParseString(input)
		if err != nil {
			t.Fatalf("%s: ParseString returned error: %v", name, err)
		}
		if !doc.HasFrontmatter() {
			t.Fatalf("%s: expected frontmatter", name)
		}
		if _, ok, err := doc.Get(testKeyTitle); err != nil || !ok {
			t.Fatalf("%s: title not readable: ok=%v err=%v", name, ok, err)
		}
		if body := string(doc.Body()); body != plainBody && body != "body\r\n" {
			t.Fatalf("%s: body = %q", name, body)
		}
	}
}

// TestUpdateFileKeepsFrontmatterAfterTrailingWhitespace is the failure a parse
// test alone would miss. If the delimiter stops being recognized, the file
// parses as bodyless frontmatter, a set-if-missing write prepends a fresh
// block, and the note's original metadata drops into the body as prose.
func TestUpdateFileKeepsFrontmatterAfterTrailingWhitespace(t *testing.T) {
	t.Parallel()

	for name, src := range map[string]string{
		"opener space": "--- \ntitle: My Note\n---\n" + plainBody,
		"opener tab":   "---\t\ntitle: My Note\n---\n" + plainBody,
	} {
		path := filepath.Join(t.TempDir(), "note.md")
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("%s: WriteFile returned error: %v", name, err)
		}

		// The write mdid does. The Has check it guards with is irrelevant
		// here: a document whose frontmatter went unread either way gets a
		// fresh block prepended.
		if err := UpdateFile(path, func(doc *Document) error {
			return doc.SetString("uid", "generated")
		}); err != nil {
			t.Fatalf("%s: UpdateFile returned error: %v", name, err)
		}

		doc, err := ReadFile(path)
		if err != nil {
			t.Fatalf("%s: ReadFile returned error: %v", name, err)
		}
		if body := string(doc.Body()); body != plainBody {
			t.Fatalf("%s: metadata was demoted into the body %q", name, body)
		}
		if title, _, err := doc.GetString(testKeyTitle); err != nil || title != "My Note" {
			t.Fatalf("%s: title = %q err=%v", name, title, err)
		}
		if uid, _, err := doc.GetString("uid"); err != nil || uid != "generated" {
			t.Fatalf("%s: uid = %q err=%v", name, uid, err)
		}
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

func TestParseDuplicateFrontmatterKeys(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("---\ntitle: one\ntitle: two\n---\nbody\n"))
	if !errors.Is(err, ErrDuplicateFrontmatterKey) {
		t.Fatalf("expected ErrDuplicateFrontmatterKey, got: %v", err)
	}
}

func TestParseDuplicateNestedFrontmatterKeys(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("---\nmeta:\n  title: one\n  title: two\n---\nbody\n"))
	if !errors.Is(err, ErrDuplicateFrontmatterKey) {
		t.Fatalf("expected ErrDuplicateFrontmatterKey, got: %v", err)
	}
}

func TestParseDuplicateNonScalarKey(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("---\n? [1, 2]\n: first\n? [1, 2]\n: second\n---\nbody\n"))
	if !errors.Is(err, ErrDuplicateFrontmatterKey) {
		t.Fatalf("expected ErrDuplicateFrontmatterKey, got: %v", err)
	}
}

// TestParseAliasChainIsLinear guards the visited-set in the duplicate-key
// validator. Each anchor here aliases its predecessor twice, so re-walking the
// alias target every time costs 2^depth: depth 28 took about 40 seconds before
// the fix. The budget is deliberately enormous, since the fixed parse is
// microseconds.
func TestParseAliasChainIsLinear(t *testing.T) {
	t.Parallel()

	var src strings.Builder
	src.WriteString("l0: &n0\n  k: 1\n")
	for i := 1; i <= 28; i++ {
		fmt.Fprintf(&src, "l%d: &n%d\n  a: *n%d\n  b: *n%d\n", i, i, i-1, i-1)
	}

	start := time.Now()
	if _, err := ParseString("---\n" + src.String() + "---\nbody\n"); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("alias chain took %v, expected a linear walk", elapsed)
	}
}

// TestParseDuplicateKeyBehindAlias keeps the visited-set honest: a mapping
// reached through an alias must still have its own duplicates checked.
func TestParseDuplicateKeyBehindAlias(t *testing.T) {
	t.Parallel()

	_, err := ParseString("---\nbase: &b\n  x: 1\n  x: 2\nuse: *b\n---\nbody\n")
	if !errors.Is(err, ErrDuplicateFrontmatterKey) {
		t.Fatalf("expected ErrDuplicateFrontmatterKey, got %v", err)
	}
}

// TestRoundTripValueContainingDelimiter is the regression test for the
// indented-marker bug. yaml.v3 writes a multi-line value as an indented block
// scalar, so an indented "---" is value content, not the end of the
// frontmatter. Before the column-0 rule the value came back truncated and the
// remainder landed in the body, with no error.
func TestRoundTripValueContainingDelimiter(t *testing.T) {
	t.Parallel()

	for _, marker := range []string{"---", "..."} {
		want := "line1\n" + marker + "\nline2"

		out, _, err := MutateString("real body\n", func(doc *Document) error {
			return doc.Set("doc", want)
		})
		if err != nil {
			t.Fatalf("%s: Mutate returned error: %v", marker, err)
		}

		doc, err := ParseString(out)
		if err != nil {
			t.Fatalf("%s: re-parsing our own output failed: %v\noutput: %q", marker, err, out)
		}

		got, ok, err := doc.GetString("doc")
		if err != nil {
			t.Fatalf("%s: GetString returned error: %v", marker, err)
		}
		if !ok || got != want {
			t.Fatalf("%s: value corrupted: got %q want %q", marker, got, want)
		}

		if body := string(doc.Body()); body != "real body\n" {
			t.Fatalf("%s: body corrupted: got %q", marker, body)
		}
	}
}

// TestRoundTripValueContainingDelimiterSurvivesSecondPass pins that a second
// mutation does not restructure the file. This is the shape a folder-walking
// tool produces when it adds a uid on a later run.
func TestRoundTripValueContainingDelimiterSurvivesSecondPass(t *testing.T) {
	t.Parallel()

	first, _, err := MutateString("real body\n", func(doc *Document) error {
		return doc.Set("doc", "line1\n---\nline2")
	})
	if err != nil {
		t.Fatalf("Mutate returned error: %v", err)
	}

	second, _, err := MutateString(first, func(doc *Document) error {
		return doc.SetString("uid", "abc")
	})
	if err != nil {
		t.Fatalf("second Mutate returned error: %v", err)
	}

	doc, err := ParseString(second)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, _, _ := doc.GetString("doc"); got != "line1\n---\nline2" {
		t.Fatalf("value corrupted after a second pass: %q", got)
	}
	if body := string(doc.Body()); body != "real body\n" {
		t.Fatalf("body corrupted after a second pass: %q", body)
	}
	if uid, ok, _ := doc.GetString("uid"); !ok || uid != "abc" {
		t.Fatalf("uid missing: ok=%v value=%q", ok, uid)
	}
}

// TestParseCommentOnlyFrontmatter pins that a commented-out or template header
// is an empty mapping, not an error. yaml.v3 reports an empty document with
// Kind left at its zero value.
func TestParseCommentOnlyFrontmatter(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"---\n# just a comment\n---\nbody\n",
		"---\n# one\n# two\n---\nbody\n",
		"---\n   # indented comment\n---\nbody\n",
	} {
		doc, err := ParseString(src)
		if err != nil {
			t.Fatalf("Parse(%q) returned error: %v", src, err)
		}
		if keys, kerr := doc.Keys(); kerr != nil || len(keys) != 0 {
			t.Fatalf("Parse(%q) keys = %v, err = %v", src, keys, kerr)
		}
		if got := string(doc.Body()); got != plainBody {
			t.Fatalf("Parse(%q) body = %q", src, got)
		}
	}
}

// TestUpdateFileCommentOnlyFrontmatter is the same case through the file path a
// folder-walking tool uses.
func TestUpdateFileCommentOnlyFrontmatter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("---\n# header disabled\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := UpdateFile(path, func(doc *Document) error {
		return doc.SetString("uid", "abc")
	}); err != nil {
		t.Fatalf("UpdateFile returned error: %v", err)
	}

	got, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if want := "---\nuid: abc\n---\nbody\n"; string(got) != want {
		t.Fatalf("unexpected content:\n got: %q\nwant: %q", string(got), want)
	}
}

// TestParseUTF8BOM pins that a byte order mark does not hide the frontmatter.
// Without the fix the first Set prepends a second block and the original
// metadata becomes body text, permanently.
func TestParseUTF8BOM(t *testing.T) {
	t.Parallel()

	doc, err := ParseString("\xEF\xBB\xBF---\ntitle: original\ntags: [a, b]\n---\nbody\n")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !doc.HasFrontmatter() {
		t.Fatalf("expected the BOM'd frontmatter to be recognized")
	}
	if got, _, _ := doc.GetString("title"); got != bomOriginalTitle {
		t.Fatalf("title = %q, want original", got)
	}
	if got := string(doc.Body()); got != plainBody {
		t.Fatalf("body = %q, want %q", got, "body\n")
	}
}

// TestUpdateFileUTF8BOMKeepsMetadata is the file-path version: adding one key
// must not strand the rest of the frontmatter in the body.
func TestUpdateFileUTF8BOMKeepsMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	original := []byte("\xEF\xBB\xBF---\ntitle: original\n---\nbody\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := UpdateFile(path, func(doc *Document) error {
		return doc.SetString("uid", "abc")
	}); err != nil {
		t.Fatalf("UpdateFile returned error: %v", err)
	}

	doc, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if got, _, _ := doc.GetString("title"); got != bomOriginalTitle {
		t.Fatalf("title = %q, want original", got)
	}
	if got := string(doc.Body()); got != plainBody {
		t.Fatalf("body = %q, want %q", got, "body\n")
	}
}

func TestParseMalformedYAMLFrontmatter(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("---\ntitle: [oops\n---\nbody\n"))
	if err == nil {
		t.Fatalf("expected parse error for malformed YAML")
	}
}

// TestParsedDocumentsDoNotShareNodes guards the struct-copy in
// parseFrontmatterMapping: the Document keeps the mapping node's slice header
// instead of deep-cloning it, so two Documents parsed from the same bytes must
// not be able to reach each other's nodes when one is mutated.
func TestParsedDocumentsDoNotShareNodes(t *testing.T) {
	t.Parallel()

	input := []byte("---\ntitle: original\n---\nbody\n")
	first := mustParse(t, input)
	second := mustParse(t, input)

	// Replace, remove, and append: deleteValueAtIndex shifts the backing
	// array in place, which would corrupt a shared node.
	mustSet(t, first, testKeyTitle, testValueNew)
	mustDelete(t, first, testKeyTitle)
	mustSet(t, first, "added", true)

	value, ok := mustGet(t, second, testKeyTitle)
	if !ok || value != bomOriginalTitle {
		t.Fatalf("second document was modified through the first: %#v (ok=%v)", value, ok)
	}

	if keys := mustKeys(t, second); !slices.Equal(keys, []string{testKeyTitle}) {
		t.Fatalf("second document keys were modified through the first: %#v", keys)
	}

	if got := mustBytes(t, second); !bytes.Equal(got, input) {
		t.Fatalf("second document bytes changed:\n got: %q\nwant: %q", string(got), string(input))
	}
}

// TestSetFrontmatterSnapshotsCallerMap guards the other half of the same
// optimisation: yaml.v3's Encode must have already copied the caller's map.
func TestSetFrontmatterSnapshotsCallerMap(t *testing.T) {
	t.Parallel()

	source := map[string]any{"a": 1}
	doc := mustParse(t, []byte("body\n"))
	if err := doc.SetFrontmatter(source); err != nil {
		t.Fatalf("SetFrontmatter returned error: %v", err)
	}

	source["a"] = 2
	source["b"] = 3

	if keys := mustKeys(t, doc); !slices.Equal(keys, []string{"a"}) {
		t.Fatalf("caller map mutation leaked into the document: %#v", keys)
	}
}

func TestSetGetDeleteKeys(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("content\n"))
	mustSet(t, doc, testKeyTitle, "Hello")
	mustSet(t, doc, "tags", []string{"go", "markdown"})

	keys := mustKeys(t, doc)
	if !slices.Equal(keys, []string{testKeyTitle, "tags"}) {
		t.Fatalf("unexpected keys: %#v", keys)
	}

	value, ok := mustGet(t, doc, testKeyTitle)
	if !ok || value != "Hello" {
		t.Fatalf("unexpected value: %#v (ok=%v)", value, ok)
	}

	deleted := mustDelete(t, doc, testKeyTitle)
	if !deleted {
		t.Fatalf("expected key to be deleted")
	}

	_, ok = mustGet(t, doc, testKeyTitle)
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

	has, err = doc.Has(testKeyTitle)
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
	mustSet(t, doc, testKeyTitle, "two")

	first := mustBytes(t, doc)

	mustSet(t, doc, testKeyTitle, "two")
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
	mustSet(t, doc, testKeyTitle, "new")

	output := mustBytes(t, doc)
	if !bytes.Contains(output, []byte("\r\n")) {
		t.Fatalf("expected CRLF newlines in output")
	}
	lfOnly := bytes.ReplaceAll(output, []byte("\r\n"), nil)
	if bytes.Contains(lfOnly, []byte("\n")) {
		t.Fatalf("expected no LF-only newlines")
	}
}

// TestDetectPreferredNewline pins the majority rule: one stray CRLF inside an
// LF file must not flip the style of a newly created frontmatter block.
func TestDetectPreferredNewline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"empty", "", lf},
		{"lf only", "a\nb\n", lf},
		{"crlf only", "a\r\nb\r\n", crlf},
		{"stray crlf in lf file", "a\nb\r\nc\n", lf},
		{"stray lf in crlf file", "a\r\nb\nc\r\n", crlf},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := detectPreferredNewline([]byte(tt.content)); got != tt.want {
				t.Fatalf("detectPreferredNewline(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestSetKeepsLFWhenStrayCRLFInBody(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("para one\ncode\r\npara two\n"))
	mustSet(t, doc, testKeyTitle, "world")

	want := "---\ntitle: world\n---\npara one\ncode\r\npara two\n"
	if got := mustBytes(t, doc); string(got) != want {
		t.Fatalf("unexpected output:\n got: %q\nwant: %q", string(got), want)
	}
}

func TestSetCreatesFrontmatterWithPreferredNewline(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\r\n"))
	mustSet(t, doc, testKeyTitle, "new")

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
		return doc.Set(testKeyTitle, "new")
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

	_, err := ReadFile(link)
	if err == nil {
		t.Fatalf("expected error when reading symlink")
	}
}

// TestReadFileNoFollowRefusesSymlink covers the second symlink guard: the
// Lstat check can be raced, so the open itself must also refuse to follow a
// final symlink component.
func TestReadFileNoFollowRefusesSymlink(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("O_NOFOLLOW is not available on Windows")
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

	if _, err := readFileNoFollow(link); err == nil {
		t.Fatalf("expected readFileNoFollow to refuse a symlink")
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

// TestSetFrontmatterRejectsEmptyKey pins that SetFrontmatter applies the same
// empty-key rule as Set. Before the check it wrote `"": value` to the user's
// file, where Get, Has, and Delete all refuse to address it, so nothing built
// on this library could ever remove it again.
func TestSetFrontmatterRejectsEmptyKey(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))
	err := doc.SetFrontmatter(map[string]any{"": "orphan", testKeyTitle: "t"})
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey, got %v", err)
	}

	out, berr := doc.Bytes()
	if berr != nil {
		t.Fatalf("Bytes returned error: %v", berr)
	}
	if strings.Contains(string(out), `"": orphan`) {
		t.Fatalf("empty key was written to the document: %q", string(out))
	}
}

// TestSetFrontmatterAcceptsOtherKeys guards the check against over-reaching:
// only an empty scalar key is rejected.
func TestSetFrontmatterAcceptsOtherKeys(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))
	if err := doc.SetFrontmatter(map[string]any{
		testKeyTitle: "t",
		"1":          "one",
		"meta":       map[string]any{"": "nested empty key is not top level"},
	}); err != nil {
		t.Fatalf("SetFrontmatter returned error: %v", err)
	}

	keys, err := doc.Keys()
	if err != nil {
		t.Fatalf("Keys returned error: %v", err)
	}
	slices.Sort(keys)
	if want := []string{"1", "meta", testKeyTitle}; !slices.Equal(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
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
		return doc.SetString(testKeyTitle, testValueNew)
	})
	if err != nil {
		t.Fatalf("Mutate returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected content to change")
	}

	updatedDoc := mustParse(t, updated)
	title, ok, err := updatedDoc.GetString(testKeyTitle)
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if !ok || title != testValueNew {
		t.Fatalf("unexpected title: %q (ok=%v)", title, ok)
	}

	second, changed, err := Mutate(updated, func(doc *Document) error {
		return doc.SetString(testKeyTitle, testValueNew)
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

// TestMutateDoesNotWriteToInput guards the promise that lets Mutate compare
// against content without copying it first.
func TestMutateDoesNotWriteToInput(t *testing.T) {
	t.Parallel()

	input := []byte("---\ntitle: old\n---\nbody\n")
	original := append([]byte(nil), input...)

	if _, _, err := Mutate(input, func(doc *Document) error {
		return doc.SetString(testKeyTitle, "new")
	}); err != nil {
		t.Fatalf("Mutate returned error: %v", err)
	}

	if !bytes.Equal(input, original) {
		t.Fatalf("Mutate modified its input:\n got: %q\nwant: %q", string(input), string(original))
	}
}

// TestDeleteReleasesRemovedNodes guards the memory contract: the vacated tail
// slots must be cleared, or a large deleted value stays reachable through the
// backing array.
func TestDeleteReleasesRemovedNodes(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\ntitle: hello\nbody: text\nkeep: yes\n---\nbody\n"))
	mustDelete(t, doc, "body")

	content := doc.frontmatter.Content
	if len(content) != 2*frontmatterKVPairWidth {
		t.Fatalf("expected two remaining keys, got %d nodes", len(content))
	}

	// The vacated tail must be cleared, otherwise the deleted value stays
	// reachable through the backing array.
	tail := content[:cap(content)][len(content):]
	for i, node := range tail {
		if node != nil {
			t.Fatalf("stale node left in backing array at index %d", len(content)+i)
		}
	}
}

func TestMutateStringHelper(t *testing.T) {
	t.Parallel()

	content := "---\r\ntitle: old\r\n---\r\nbody\r\n"
	updated, changed, err := MutateString(content, func(doc *Document) error {
		return doc.SetString(testKeyTitle, testValueNew)
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

func TestIntegrationSetIfMissingFlow(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\ntitle: note\n---\nbody\n"))

	hasUID, err := doc.Has("uid")
	if err != nil {
		t.Fatalf("Has returned error: %v", err)
	}
	if hasUID {
		t.Fatalf("expected uid to be absent in test fixture")
	}

	if err = doc.SetString("uid", "abc-123"); err != nil {
		t.Fatalf("SetString returned error: %v", err)
	}

	uid, ok, err := doc.GetString("uid")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if !ok || uid != "abc-123" {
		t.Fatalf("unexpected uid: %q (ok=%v)", uid, ok)
	}
}

func TestIntegrationReplaceMetadataFlow(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\ntitle: note\nfingerprint: old\n---\nbody\n"))
	if err := doc.SetString("fingerprint", testValueNew); err != nil {
		t.Fatalf("SetString returned error: %v", err)
	}

	fingerprint, ok, err := doc.GetString("fingerprint")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if !ok || fingerprint != testValueNew {
		t.Fatalf("unexpected fingerprint: %q (ok=%v)", fingerprint, ok)
	}

	keys := mustKeys(t, doc)
	if !slices.Equal(keys, []string{testKeyTitle, "fingerprint"}) {
		t.Fatalf("unexpected key order after replace: %#v", keys)
	}
}

func TestIntegrationRemoveThenAddFlow(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\ntitle: note\nfingerprint: old\n---\nbody\n"))

	deleted := mustDelete(t, doc, "fingerprint")
	if !deleted {
		t.Fatalf("expected fingerprint to be deleted")
	}

	if err := doc.SetString("fingerprint", testValueNew); err != nil {
		t.Fatalf("SetString returned error: %v", err)
	}

	fingerprint, ok, err := doc.GetString("fingerprint")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if !ok || fingerprint != testValueNew {
		t.Fatalf("unexpected fingerprint: %q (ok=%v)", fingerprint, ok)
	}
}

func TestParseString(t *testing.T) {
	t.Parallel()

	doc, err := ParseString("---\ntitle: hello\n---\nbody\n")
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	title, ok, err := doc.GetString(testKeyTitle)
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if !ok || title != testValueHello {
		t.Fatalf("unexpected title: %q (ok=%v)", title, ok)
	}
}

func TestSetBody(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\ntitle: hello\n---\nbody\n"))
	doc.SetBody([]byte("updated\n"))

	if got := string(doc.Body()); got != "updated\n" {
		t.Fatalf("unexpected body: %q", got)
	}
}

// TestDocumentCopiesBytes pins the three ownership promises the godoc makes.
// Each clone looks redundant and each is load-bearing: without it a caller
// holding a reference it does not own can change the document underneath it.
func TestDocumentCopiesBytes(t *testing.T) {
	t.Parallel()

	t.Run("Body returns a copy", func(t *testing.T) {
		t.Parallel()

		doc := mustParse(t, []byte("---\ntitle: hello\n---\nbody\n"))
		got := doc.Body()
		got[0] = 'X'
		if again := string(doc.Body()); again != plainBody {
			t.Fatalf("mutating the returned slice changed the document: %q", again)
		}
	})

	t.Run("SetBody stores a copy", func(t *testing.T) {
		t.Parallel()

		source := []byte("original\n")
		doc := mustParse(t, []byte("placeholder\n"))
		doc.SetBody(source)
		source[0] = 'X'
		if got := string(doc.Body()); got != "original\n" {
			t.Fatalf("mutating the source slice changed the document: %q", got)
		}
	})

	t.Run("Parse does not alias its input", func(t *testing.T) {
		t.Parallel()

		input := []byte("---\ntitle: hello\n---\nbody\n")
		doc, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse returned error: %v", err)
		}
		input[len(input)-1] = 'X'
		if got := string(doc.Body()); got != plainBody {
			t.Fatalf("mutating the input buffer changed the document: %q", got)
		}
	})
}

func TestFrontmatterReturnsMap(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("---\ntitle: hello\ncount: 2\n---\nbody\n"))

	frontmatter, err := doc.Frontmatter()
	if err != nil {
		t.Fatalf("Frontmatter returned error: %v", err)
	}
	if frontmatter[testKeyTitle] != testValueHello {
		t.Fatalf("unexpected title: %#v", frontmatter[testKeyTitle])
	}
	if frontmatter["count"] != 2 {
		t.Fatalf("unexpected count: %#v", frontmatter["count"])
	}
}

func TestFrontmatterNilDocument(t *testing.T) {
	t.Parallel()

	var doc *Document
	frontmatter, err := doc.Frontmatter()
	if err != nil {
		t.Fatalf("Frontmatter returned error: %v", err)
	}
	if len(frontmatter) != 0 {
		t.Fatalf("expected empty map, got %#v", frontmatter)
	}
}

func TestWriteFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	doc := mustParse(t, []byte("---\ntitle: hello\n---\nbody\n"))

	if err := doc.WriteFile(path, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	content, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "---\ntitle: hello\n---\nbody\n" {
		t.Fatalf("unexpected written content: %q", string(content))
	}
}

func TestWriteFileRefusesSymlink(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("symlink permissions can vary on Windows runners")
	}

	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	link := filepath.Join(dir, "link.md")
	doc := mustParse(t, []byte("---\ntitle: hello\n---\nbody\n"))

	if err := os.WriteFile(target, []byte("old\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	err := doc.WriteFile(link, 0o600)
	if err == nil {
		t.Fatalf("expected error when writing symlink")
	}

	info, statErr := os.Lstat(link)
	if statErr != nil {
		t.Fatalf("Lstat returned error: %v", statErr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink to remain in place, mode=%v", info.Mode())
	}

	content, readErr := os.ReadFile(target) // #nosec G304 -- path is created by t.TempDir in this test.
	if readErr != nil {
		t.Fatalf("ReadFile returned error: %v", readErr)
	}
	if string(content) != "old\n" {
		t.Fatalf("unexpected target content: %q", string(content))
	}
}

// TestWriteFileRefusesFIFO keeps the three file entry points consistent:
// ReadFile and UpdateFile already refuse a non-regular file, so WriteFile does
// too rather than silently replacing it with a regular one.
func TestWriteFileRefusesFIFO(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("named pipes need elevated privileges on Windows")
	}

	t.Parallel()

	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := mkfifo(fifo); err != nil {
		t.Skipf("cannot create a FIFO on this platform: %v", err)
	}

	doc := mustParse(t, []byte("---\nk: v\n---\nbody\n"))
	err := doc.WriteFile(fifo, 0o600)
	if err == nil {
		t.Fatalf("expected WriteFile to refuse a FIFO")
	}
	if !strings.Contains(err.Error(), "refusing to write non-regular file") {
		t.Fatalf("expected non-regular file refusal, got %v", err)
	}

	info, statErr := os.Lstat(fifo)
	if statErr != nil {
		t.Fatalf("Lstat returned error: %v", statErr)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("expected the FIFO to remain in place, mode=%v", info.Mode())
	}
}

func TestWriteFileEmptyPath(t *testing.T) {
	t.Parallel()

	doc := mustParse(t, []byte("body\n"))
	if err := doc.WriteFile("", 0o600); !errors.Is(err, errPathEmpty) {
		t.Fatalf("expected errPathEmpty, got: %v", err)
	}
}

func TestReadFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("---\ntitle: hello\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	doc, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	title, ok, err := doc.GetString(testKeyTitle)
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if !ok || title != testValueHello {
		t.Fatalf("unexpected title: %q (ok=%v)", title, ok)
	}
}

func TestReadFileValidation(t *testing.T) {
	t.Parallel()

	_, err := ReadFile("")
	if !errors.Is(err, errPathEmpty) {
		t.Fatalf("expected errPathEmpty, got: %v", err)
	}
}

func TestReadFileParseError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(path, []byte("---\ntitle: [oops\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := ReadFile(path)
	if err == nil || !strings.Contains(err.Error(), "failed to parse markdown") {
		t.Fatalf("expected wrapped parse error, got: %v", err)
	}
}

func TestReadFileDuplicateKeyError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "duplicate.md")
	if err := os.WriteFile(path, []byte("---\ntitle: one\ntitle: two\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := ReadFile(path)
	if !errors.Is(err, ErrDuplicateFrontmatterKey) {
		t.Fatalf("expected ErrDuplicateFrontmatterKey, got: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to parse markdown") {
		t.Fatalf("expected wrapped parse error, got: %v", err)
	}
}

func TestReadRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("body\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	content, perm, err := readRegularFile(path)
	if err != nil {
		t.Fatalf("readRegularFile returned error: %v", err)
	}
	if string(content) != plainBody {
		t.Fatalf("unexpected content: %q", string(content))
	}
	// Windows has no Unix permission bits: Go reports 0666 for any writable
	// file there, so the mode carries no information to assert.
	if runtime.GOOS != windowsOS && perm != 0o600 {
		t.Fatalf("unexpected perm: %#o", perm)
	}
}

// TestReadRegularFileRefuses covers the symlink and non-regular refusals. Both
// ReadFile and UpdateFile route through this one helper, so one test pins both
// entry points.
func TestReadRegularFileRefuses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	if err := os.WriteFile(target, []byte("body\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := readRegularFile(link); err == nil ||
		!strings.Contains(err.Error(), "refusing to read symlink") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}

	if _, _, err := readRegularFile(dir); err == nil ||
		!strings.Contains(err.Error(), "refusing to read non-regular file") {
		t.Fatalf("expected non-regular refusal, got %v", err)
	}
}

// TestRestoreFile covers the undo that runs when a write stops partway. The
// write that triggers it cannot be forced portably, but the undo itself can:
// the file below is exactly what a partway write leaves behind, the new prefix
// in front of the old tail.
func TestRestoreFile(t *testing.T) {
	t.Parallel()

	const original = "---\ntitle: original\n---\nbody\n"
	const mixed = "---\ntitle: new, and considerably longer than the original title\n---\nbody\n"

	path := filepath.Join(t.TempDir(), "restored.md")
	if err := os.WriteFile(path, []byte(mixed), 0o600); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	if err := restoreFile(path, []byte(original)); err != nil {
		t.Fatalf("restoreFile returned error: %v", err)
	}

	got, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(got) != original {
		t.Errorf("restoreFile left %q, want %q", got, original)
	}
}

func TestWriteFileInPlace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "inplace.md")

	if err := writeFileInPlace(path, []byte("content\n"), 0o600, nil); err != nil {
		t.Fatalf("writeFileInPlace returned error: %v", err)
	}

	content, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "content\n" {
		t.Fatalf("unexpected content: %q", string(content))
	}
}

func TestWriteFileInPlaceValidationError(t *testing.T) {
	t.Parallel()

	err := writeFileInPlace("", []byte("x"), 0o600, nil)
	if err == nil {
		t.Fatalf("expected error for empty path")
	}
}

// TestWriteFileInPlaceShortens pins the truncate step. Without it a shorter
// payload would leave the tail of the previous content on disk.
func TestWriteFileInPlaceShortens(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "note.md")
	if err := writeFileInPlace(path, []byte("a long first version\n"), 0o600, nil); err != nil {
		t.Fatalf("writeFileInPlace returned error: %v", err)
	}
	if err := writeFileInPlace(path, []byte("short\n"), 0o600, nil); err != nil {
		t.Fatalf("writeFileInPlace returned error: %v", err)
	}

	content, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "short\n" {
		t.Fatalf("stale tail left behind: %q", string(content))
	}
}

// TestWriteFileInPlaceRefusesSymlink pins O_NOFOLLOW on the write path. The
// read side has an equivalent test; without this one, dropping |noFollow from
// the open flags looks like harmless tidying, because Document.WriteFile and
// UpdateFile both Lstat first, and the suite stays green.
func TestWriteFileInPlaceRefusesSymlink(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("O_NOFOLLOW is not available on Windows")
	}

	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	link := filepath.Join(dir, "link.md")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := writeFileInPlace(link, []byte("CLOBBERED\n"), 0o600, nil); err == nil {
		t.Fatalf("expected writeFileInPlace to refuse a symlink")
	}

	got, err := os.ReadFile(target) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(got) != "original\n" {
		t.Fatalf("the symlink target was written through: %q", string(got))
	}
}

// TestWriteFileInPlaceOpenError covers the open failure, which is the branch
// every other error path is unreachable without.
func TestWriteFileInPlaceOpenError(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("file permissions do not gate writes the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("body\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}
	defer func() { _ = os.Chmod(path, 0o600) }()

	if err := writeFileInPlace(path, []byte("changed\n"), 0o600, nil); err == nil {
		t.Fatalf("expected an error opening a read-only file")
	}

	content, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "body\n" {
		t.Fatalf("a failed open must not touch the file: %q", string(content))
	}
}

// TestUpdateFilePreservesInodeAndHardLinks guards the data loss mdid reported:
// writing through a temporary file and renaming it over the target replaced the
// inode, so extended attributes, ownership and file flags were dropped and a
// hard-linked file split into divergent copies. Writing the existing inode keeps
// all of them, and os.SameFile covers the whole attribute store rather than
// xattrs alone.
func TestUpdateFilePreservesInodeAndHardLinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	link := filepath.Join(dir, "link.md")

	if err := os.WriteFile(path, []byte("body\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.Link(path, link); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	before, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat returned error: %v", err)
	}

	if err = UpdateFile(path, func(doc *Document) error {
		return doc.SetString(testKeyTitle, "linked")
	}); err != nil {
		t.Fatalf("UpdateFile returned error: %v", err)
	}

	after, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat returned error: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatalf("UpdateFile replaced the inode, dropping its attributes and hard links")
	}

	content, err := os.ReadFile(link) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "---\ntitle: linked\n---\nbody\n" {
		t.Fatalf("hard link did not observe the update: %q", string(content))
	}
}

// TestDeleteRefusesAnchorInUse guards the data loss this was written for: a
// key that defines an anchor and is aliased by another key cannot be deleted,
// because the resulting file holds a dangling alias and can never be parsed
// again.
func TestDeleteRefusesAnchorInUse(t *testing.T) {
	t.Parallel()

	doc, err := ParseString("---\nbase: &b v\nuse: *b\n---\nbody\n")
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	removed, err := doc.Delete("base")
	if !errors.Is(err, ErrAnchorInUse) {
		t.Fatalf("expected ErrAnchorInUse, got %v", err)
	}
	if removed {
		t.Fatalf("a refused delete must report removed=false")
	}

	// The document must still serialize to something parseable.
	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if _, err = ParseString(string(out)); err != nil {
		t.Fatalf("refused delete left unparseable frontmatter: %v", err)
	}
}

// TestAnchorGuardFindsNestedAlias covers the alias search recursing into
// another key's value. Every other anchor fixture puts the alias at the top
// level of the mapping, which the loop's first iteration finds without
// recursing, so this is the only thing standing behind that recursive call.
func TestAnchorGuardFindsNestedAlias(t *testing.T) {
	t.Parallel()

	for name, src := range map[string]string{
		"nested mapping":  "---\nbase: &x 1\nholder:\n  ref: *x\n---\nbody\n",
		"nested sequence": "---\nbase: &x 1\nholder:\n  - *x\n---\nbody\n",
		"deeply nested":   "---\nbase: &x 1\nholder:\n  mid:\n    ref: *x\n---\nbody\n",
	} {
		doc, err := ParseString(src)
		if err != nil {
			t.Fatalf("%s: ParseString returned error: %v", name, err)
		}

		if _, err = doc.Delete("base"); !errors.Is(err, ErrAnchorInUse) {
			t.Fatalf("%s: Delete expected ErrAnchorInUse, got %v", name, err)
		}
		if err = doc.Set("base", 2); !errors.Is(err, ErrAnchorInUse) {
			t.Fatalf("%s: Set expected ErrAnchorInUse, got %v", name, err)
		}

		out, err := doc.Bytes()
		if err != nil {
			t.Fatalf("%s: Bytes returned error: %v", name, err)
		}
		if _, err = ParseString(string(out)); err != nil {
			t.Fatalf("%s: refused mutation left unparseable frontmatter: %v", name, err)
		}
	}
}

// TestDeleteUnreferencedAnchor covers the other side: an anchor nothing points
// at is deletable.
func TestDeleteUnreferencedAnchor(t *testing.T) {
	t.Parallel()

	doc, err := ParseString("---\nbase: &b v\nplain: w\n---\nbody\n")
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	removed, err := doc.Delete("base")
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if !removed {
		t.Fatalf("expected the unreferenced anchor to be removed")
	}

	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if _, err = ParseString(string(out)); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
}

// TestUpdateFileRefusesAnchorDelete is the file-path version: the write must
// not happen at all, so the file on disk is untouched.
func TestUpdateFileRefusesAnchorDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	original := "---\nbase: &b v\nuse: *b\n---\nbody\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	err := UpdateFile(path, func(doc *Document) error {
		_, err := doc.Delete("base")
		return err
	})
	if !errors.Is(err, ErrAnchorInUse) {
		t.Fatalf("expected ErrAnchorInUse, got %v", err)
	}

	content, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != original {
		t.Fatalf("refused delete still wrote the file: %q", string(content))
	}
}

// TestSetRefusesAnchorInUse covers the other door into the same data loss:
// overwriting an anchor-defining value with Set must be refused exactly like
// Delete is, or the file is written unparseable.
func TestSetRefusesAnchorInUse(t *testing.T) {
	t.Parallel()

	doc, err := ParseString("---\nbase: &b v\nuse: *b\n---\nbody\n")
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	if err = doc.Set("base", "plain"); !errors.Is(err, ErrAnchorInUse) {
		t.Fatalf("expected ErrAnchorInUse, got %v", err)
	}

	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if _, err = ParseString(string(out)); err != nil {
		t.Fatalf("refused Set left unparseable frontmatter: %v", err)
	}

	// Once the alias is gone the same Set succeeds.
	if _, err = doc.Delete("use"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if err = doc.Set("base", "plain"); err != nil {
		t.Fatalf("unexpected error after the alias was gone: %v", err)
	}
}

// TestDeleteRefusesAnchoredKeyNode covers YAML letting the anchor sit on the
// key (`&b base: v`) rather than the value. The guard scans both nodes of the
// pair, so this is refused instead of writing a dangling alias.
func TestDeleteRefusesAnchoredKeyNode(t *testing.T) {
	t.Parallel()

	doc, err := ParseString("---\n&b base: v\nuse: *b\n---\nbody\n")
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	if _, err = doc.Delete("base"); !errors.Is(err, ErrAnchorInUse) {
		t.Fatalf("expected ErrAnchorInUse for an anchored key, got %v", err)
	}

	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if _, err = ParseString(string(out)); err != nil {
		t.Fatalf("refused Delete left unparseable frontmatter: %v", err)
	}
}

// TestDeleteAllowsSelfContainedAnchor is the other side: when an anchor and
// its only alias both live in the subtree being dropped, they go away together
// and the mutation is safe. The pair must be excluded from the alias search.
func TestDeleteAllowsSelfContainedAnchor(t *testing.T) {
	t.Parallel()

	for name, src := range map[string]string{
		"mapping":  "---\nt:\n  a: &x 1\n  b: *x\nz: 2\n---\nbody\n",
		"sequence": "---\nt:\n  - &x 1\n  - *x\nz: 2\n---\nbody\n",
	} {
		doc, err := ParseString(src)
		if err != nil {
			t.Fatalf("%s: ParseString returned error: %v", name, err)
		}

		if _, err = doc.Delete("t"); err != nil {
			t.Fatalf("%s: deleting a self-contained anchor must be allowed: %v", name, err)
		}

		out, err := doc.Bytes()
		if err != nil {
			t.Fatalf("%s: Bytes returned error: %v", name, err)
		}
		if _, err = ParseString(string(out)); err != nil {
			t.Fatalf("%s: result does not re-parse: %v", name, err)
		}
		if strings.Contains(string(out), "*x") {
			t.Fatalf("%s: alias survived: %q", name, string(out))
		}
	}
}

// TestDeleteRefusesNestedAnchor covers an anchor defined inside the value
// being dropped, not on the value node itself.
func TestDeleteRefusesNestedAnchor(t *testing.T) {
	t.Parallel()

	doc, err := ParseString("---\ntop:\n  base: &b v\nuse: *b\n---\nbody\n")
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	if _, err = doc.Delete("top"); !errors.Is(err, ErrAnchorInUse) {
		t.Fatalf("expected ErrAnchorInUse for a nested anchor, got %v", err)
	}

	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if _, err = ParseString(string(out)); err != nil {
		t.Fatalf("refused Delete left unparseable frontmatter: %v", err)
	}
}

// TestSetPreservesCarriageReturn pins that a value holding a CR survives a
// write. yaml.v3 escapes it as \r on encode, so refusing it here would reject
// data the library can store losslessly.
func TestSetPreservesCarriageReturn(t *testing.T) {
	t.Parallel()

	doc, err := ParseString("---\nk: v\n---\nbody\n")
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	if err = doc.Set("k", "a\rb"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}

	back, err := ParseString(string(out))
	if err != nil {
		t.Fatalf("written frontmatter does not re-parse: %v", err)
	}
	got, ok, err := back.GetString("k")
	if err != nil || !ok {
		t.Fatalf("GetString: got=%q ok=%v err=%v", got, ok, err)
	}
	if got != "a\rb" {
		t.Fatalf("carriage return was not preserved: %q", got)
	}
}

// TestWriteFileRecreatesWithPerm guards the mode of a file that has to be
// recreated: a zero perm would leave it unreadable with no error reported.
func TestWriteFileRecreatesWithPerm(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "note.md")
	doc := mustParse(t, []byte("---\ntitle: a\n---\nbody\n"))
	if err := doc.WriteFile(path, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat returned error: %v", err)
	}
	// Windows reports 0666 for any writable file, so the mode proves nothing
	// there; the guard for a mode-0000 recreate is exercised on unix only.
	if runtime.GOOS != windowsOS && info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected perm: %#o", info.Mode().Perm())
	}
}

// TestNodeFromValueTypes pins the encoder across the shapes a downstream tool
// is likely to pass, so a yaml.v3 marshaling quirk cannot reach a user file
// unnoticed.
func TestNodeFromValueTypes(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]any{
		"nil":            nil,
		"int":            42,
		"float":          1.5,
		"bool":           true,
		"string":         "hi",
		"string slice":   []string{"a", "b"},
		"string map":     map[string]any{"inner": "v"},
		"numeric string": "42",
		"nested":         map[string]any{"a": map[string]any{"b": "c"}},
	} {
		assertValueRoundTrips(t, name, value)
	}
}

// assertValueRoundTrips checks the contract mdfm owns: what it writes must
// parse back to the value it was given, with the body and key intact. The
// exact emitted bytes belong to yaml.v3 and are pinned separately.
func assertValueRoundTrips(t *testing.T, name string, value any) {
	t.Helper()

	doc, err := ParseString("body\n")
	if err != nil {
		t.Fatalf("%s: ParseString returned error: %v", name, err)
	}
	if err = doc.Set("k", value); err != nil {
		t.Fatalf("%s: Set returned error: %v", name, err)
	}
	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("%s: Bytes returned error: %v", name, err)
	}

	back, err := ParseString(string(out))
	if err != nil {
		t.Fatalf("%s: written frontmatter does not re-parse: %v", name, err)
	}
	if string(back.Body()) != "body\n" {
		t.Fatalf("%s: body was lost: %q", name, string(back.Body()))
	}

	// A found value also proves the key survived under its own name. yaml
	// decodes into `any`, so a slice comes back as []any rather than []string;
	// compare rendered form.
	decoded, found, err := back.Get("k")
	if err != nil || !found || fmt.Sprint(decoded) != fmt.Sprint(value) {
		t.Fatalf("%s: round-tripped %v found=%v err=%v, want %v", name, decoded, found, err, value)
	}
}

// TestNodeFromValueEmittedBytes pins the exact bytes for the shapes that carry
// a promise mdfm makes about the file it writes: a plain string stays a plain
// scalar, a numeric-looking string keeps its quotes so it re-reads as a
// string, and the delimiters and body are untouched. Everything else about
// yaml.v3's output is deliberately not asserted.
func TestNodeFromValueEmittedBytes(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		value any
		want  string
	}{
		"string":         {"hi", "---\nk: hi\n---\nbody\n"},
		"numeric string": {"42", "---\nk: \"42\"\n---\nbody\n"},
		"nil":            {nil, "---\nk: null\n---\nbody\n"},
	} {
		doc, err := ParseString("body\n")
		if err != nil {
			t.Fatalf("%s: ParseString returned error: %v", name, err)
		}
		if err = doc.Set("k", tc.value); err != nil {
			t.Fatalf("%s: Set returned error: %v", name, err)
		}
		out, err := doc.Bytes()
		if err != nil {
			t.Fatalf("%s: Bytes returned error: %v", name, err)
		}
		if got := string(out); got != tc.want {
			t.Fatalf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// TestNodeFromValueUnencodable pins the panic recovery in encodeYAMLNode.
// yaml.v3 panics on types it cannot encode, so this is the only reason a
// caller's Set returns an error instead of taking their process down.
func TestNodeFromValueUnencodable(t *testing.T) {
	t.Parallel()

	doc, err := ParseString("---\nk: v\n---\nbody\n")
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	for name, value := range map[string]any{
		"func": func() {},
		"chan": make(chan int),
	} {
		if err = doc.Set("k", value); err == nil {
			t.Fatalf("%s: expected an error for an unencodable value", name)
		}
	}

	got, _, err := doc.GetString("k")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if got != "v" {
		t.Fatalf("a refused Set changed the value: %q", got)
	}
}

// TestBytesPreservesBOMAndAltMarker pins the two reformatting cases that used
// to rewrite a user's file on a no-op: the BOM is stripped for parsing but must
// be written back, and a "..." closing delimiter must survive.
func TestBytesPreservesBOMAndAltMarker(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"bom":          "\xEF\xBB\xBF---\ntitle: a\n---\nbody\n",
		"bom no front": "\xEF\xBB\xBFplain body\n",
		"alt marker":   "---\ntitle: a\n...\nbody\n",
		"bom + alt":    "\xEF\xBB\xBF---\ntitle: a\n...\nbody\n",
	} {
		out, changed, err := MutateString(input, func(*Document) error { return nil })
		if err != nil {
			t.Fatalf("%s: MutateString returned error: %v", name, err)
		}
		if out != input {
			t.Fatalf("%s: no-op rewrite changed the file\n  in =%q\n  out=%q", name, input, out)
		}
		if changed {
			t.Fatalf("%s: no-op rewrite reported changed=true", name)
		}
	}
}

func TestNodeFromValueNil(t *testing.T) {
	t.Parallel()

	node, err := nodeFromValue(nil)
	if err != nil {
		t.Fatalf("nodeFromValue returned error: %v", err)
	}
	if node == nil {
		t.Fatalf("expected non-nil node")
	}
	if node.Tag != "!!null" {
		t.Fatalf("unexpected tag: %q", node.Tag)
	}
}

func TestNodeFromValueScalarAndMap(t *testing.T) {
	t.Parallel()

	scalarNode, err := nodeFromValue("hello")
	if err != nil {
		t.Fatalf("nodeFromValue returned error: %v", err)
	}
	if scalarNode.Kind != yaml.ScalarNode {
		t.Fatalf("expected scalar kind, got %v", scalarNode.Kind)
	}

	mapNode, err := nodeFromValue(map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("nodeFromValue returned error: %v", err)
	}
	if mapNode.Kind != yaml.MappingNode {
		t.Fatalf("expected mapping kind, got %v", mapNode.Kind)
	}
}

// TestUpdateFileNoOpDoesNotRewrite proves the no-op path skips the write
// instead of relying on a timestamp comparison, which can pass for the wrong
// reason on a filesystem with coarse timestamp granularity. A rewrite opens the
// target for writing, so a read-only file is a deterministic barrier.
func TestUpdateFileNoOpDoesNotRewrite(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("file permissions do not gate writes the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")

	content := []byte("---\ntitle: same\n---\nbody\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}
	defer func() { _ = os.Chmod(path, 0o600) }()

	if err := UpdateFile(path, func(doc *Document) error {
		return doc.SetString(testKeyTitle, "same")
	}); err != nil {
		t.Fatalf("a no-op update must not write, got error: %v", err)
	}

	got, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("unexpected content: %q", string(got))
	}
}

func TestUpdateFileValidationAndMutationError(t *testing.T) {
	t.Parallel()

	if err := UpdateFile("", nil); !errors.Is(err, errPathEmpty) {
		t.Fatalf("expected errPathEmpty, got: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("body\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	mutateErr := errors.New("mutate failed")
	err := UpdateFile(path, func(*Document) error { return mutateErr })
	if !errors.Is(err, mutateErr) {
		t.Fatalf("expected wrapped mutate error, got: %v", err)
	}
}

func TestUpdateFileParseError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(path, []byte("---\ntitle: [oops\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	err := UpdateFile(path, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to parse markdown") {
		t.Fatalf("expected wrapped parse error, got: %v", err)
	}
}

func TestUpdateFileDuplicateKeyError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "duplicate.md")
	original := []byte("---\ntitle: one\ntitle: two\n---\nbody\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	err := UpdateFile(path, func(doc *Document) error {
		return doc.SetString(testKeyTitle, "new")
	})
	if !errors.Is(err, ErrDuplicateFrontmatterKey) {
		t.Fatalf("expected ErrDuplicateFrontmatterKey, got: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to parse markdown") {
		t.Fatalf("expected wrapped parse error, got: %v", err)
	}

	content, readErr := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if readErr != nil {
		t.Fatalf("ReadFile returned error: %v", readErr)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("expected file content to remain unchanged, got %q", string(content))
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
