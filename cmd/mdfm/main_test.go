package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inful/mdfm"
)

// TestRunSetPreservesValueWhitespace pins the CLI contract that only the key
// is trimmed. A value's leading and trailing spaces are user data.
func TestRunSetPreservesValueWhitespace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("---\ntitle: old\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{setFlag, "title=  spaced  ", path}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d, stderr=%q", exitCode, stderr.String())
	}

	doc, err := mdfm.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	got, ok, err := doc.GetString("title")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected title key to be present")
	}
	if got != "  spaced  " {
		t.Fatalf("value whitespace was altered: %q", got)
	}
}

// TestRunSetTrimsKeyWhitespace pins the other half: an accidental space around
// the key is still trimmed so `--set " title=x"` targets the same key.
func TestRunSetTrimsKeyWhitespace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("---\ntitle: old\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{setFlag, "  title=new", path}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d, stderr=%q", exitCode, stderr.String())
	}

	doc, err := mdfm.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	got, _, err := doc.GetString("title")
	if err != nil {
		t.Fatalf("GetString returned error: %v", err)
	}
	if got != "new" {
		t.Fatalf("unexpected title: %q", got)
	}
}

// TestRunAppliesSetsBeforeDeletes pins the documented order: every --set is
// applied before every --delete, regardless of the order on the command line.
// A key named by both flags is therefore removed.
func TestRunAppliesSetsBeforeDeletes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("---\ndraft: true\ntitle: keep\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	// --delete comes first on the command line, but the delete still runs last.
	exitCode := run([]string{deleteFlag, "draft", setFlag, "draft=false", path}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d, stderr=%q", exitCode, stderr.String())
	}

	doc, err := mdfm.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if ok, err := doc.Has("draft"); err != nil {
		t.Fatalf("Has returned error: %v", err)
	} else if ok {
		t.Fatalf("expected draft to be removed: deletes apply after sets")
	}
	if title, _, err := doc.GetString("title"); err != nil || title != "keep" {
		t.Fatalf("unexpected title: %q (err=%v)", title, err)
	}
}

func TestRunVersion(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{"-version"}, stdout, stderr)

	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if !strings.Contains(stdout.String(), version) {
		t.Fatalf("expected version in stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunUsageError(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run(nil, stdout, stderr)

	if exitCode != exitUsage {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("expected usage message, got %q", stderr.String())
	}
}

func TestRunUpdatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("---\ntitle: old\ndraft: true\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{setFlag, "title=new", deleteFlag, "draft", path}, stdout, stderr)

	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d, stderr=%q", exitCode, stderr.String())
	}

	content, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "title: new") {
		t.Fatalf("expected updated title, got %q", got)
	}
	if strings.Contains(got, "draft:") {
		t.Fatalf("expected draft key to be removed, got %q", got)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("expected no output, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunInvalidSetValue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("body\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{setFlag, "invalid", path}, stdout, stderr)

	if exitCode != 1 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "invalid --set value") {
		t.Fatalf("expected invalid set error, got %q", stderr.String())
	}
}

func TestRunDuplicateKeyFrontmatter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	original := []byte("---\ntitle: one\ntitle: two\n---\nbody\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{setFlag, "title=new", path}, stdout, stderr)

	if exitCode != 1 {
		t.Fatalf("unexpected exit code: %d, stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "failed to parse markdown") {
		t.Fatalf("expected parse failure, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "duplicate mapping keys") {
		t.Fatalf("expected duplicate-key error, got %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}

	content, err := os.ReadFile(path) // #nosec G304 -- path is created by t.TempDir in this test.
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("expected file content to remain unchanged, got %q", string(content))
	}
}

// TestRunUnknownFlag covers the flagSet.Parse error path: an unknown flag
// causes the flag package to write an error to stderr and run returns 1.
func TestRunUnknownFlag(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{"--bogus"}, stdout, stderr)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
}

// TestRunSetEmptyKey covers the doc.Set error path inside the mutation
// callback: an empty key (e.g. `--set =value`) produces an ErrEmptyKey that
// is wrapped and surfaced to the user.
func TestRunSetEmptyKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("body\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{setFlag, "=value", path}, stdout, stderr)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d, stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "frontmatter key cannot be empty") {
		t.Fatalf("expected empty-key error, got %q", stderr.String())
	}
}

// TestRunDeleteEmptyKey covers the doc.Delete error path: an empty key
// produces an ErrEmptyKey that is wrapped and surfaced.
func TestRunDeleteEmptyKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("---\ntitle: x\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{deleteFlag, "", path}, stdout, stderr)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d, stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "frontmatter key cannot be empty") {
		t.Fatalf("expected empty-key error, got %q", stderr.String())
	}
}
