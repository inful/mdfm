package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepeatedFlag(t *testing.T) {
	t.Parallel()

	var values repeatedFlag
	if err := values.Set("title=hello"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := values.Set("draft=false"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	if got := values.String(); got != "title=hello,draft=false" {
		t.Fatalf("unexpected String result: %q", got)
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
	exitCode := run([]string{"--set", "title=new", "--delete", "draft", path}, stdout, stderr)

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
	exitCode := run([]string{"--set", "invalid", path}, stdout, stderr)

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
	exitCode := run([]string{"--set", "title=new", path}, stdout, stderr)

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
