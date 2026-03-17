# mdfm

`mdfm` is a small utility library for one purpose: reading, manipulating, and
updating YAML frontmatter in markdown files.

Scope:

- Frontmatter parsing, semantic key operations, serialization, and safe file updates.
- This package does not implement metadata business logic such as UID generation
	or fingerprint calculation.

## Operation Guarantees

- `Set` updates an existing top-level key in place.
- `Set` appends a missing top-level key.
- `Get` and `Has` return `found=false` for missing keys.
- `Delete` removes only the requested top-level key and reports whether it was removed.
- Repeating `Set` with the same value is idempotent at the byte level.
- Repeating `Delete` on a missing key is idempotent at the byte level.
- Newline style is preserved (`LF` vs `CRLF`) when parsing and mutating documents.
- Frontmatter must be a YAML mapping with unique mapping keys for semantic key operations.

It is built to be fast, robust, and correct:

- Delimiter scanning is line-based and allocation-light.
- Frontmatter is parsed with `yaml.v3` for full YAML correctness.
- Duplicate mapping keys are rejected during parse.
- File updates are atomic and skip writes when content is unchanged.
- Symlink reads/updates are refused for safer file operations.

## Install

```bash
go get github.com/inful/mdfm
```

## Public API

Core parsing and mutation helpers:

- `Parse` and `ParseString` parse markdown content into a `Document`.
- `Mutate` and `MutateString` parse, mutate, and serialize content in one step.
- `ReadFile` parses a markdown file and refuses symlinks.
- `UpdateFile` mutates a markdown file atomically and skips the write when bytes are unchanged.

`Document` accessors and mutation methods:

- `HasFrontmatter`, `Body`, `SetBody`, `Bytes`, and `WriteFile` work with whole-document state.
- `Frontmatter` and `SetFrontmatter` read or replace the full top-level frontmatter mapping.
- `Get`, `Has`, `GetString`, `Set`, `SetString`, `Delete`, and `Keys` provide top-level key operations.

Relevant exported errors:

- `ErrUnclosedFrontmatter` when an opening delimiter has no closing delimiter.
- `ErrFrontmatterNotMapping` when frontmatter is valid YAML but not a top-level mapping.
- `ErrDuplicateFrontmatterKey` when a mapping contains duplicate keys.
- `ErrEmptyKey` when a semantic key operation receives an empty key.

## Library Usage

```go
package main

import (
	"fmt"

	"github.com/inful/mdfm"
)

func main() {
	doc, err := mdfm.ParseString("---\ntitle: Note\n---\nBody\n")
	if err != nil {
		panic(err)
	}

	if err = doc.Set("title", "Updated Note"); err != nil {
		panic(err)
	}
	if err = doc.Set("tags", []string{"go", "markdown"}); err != nil {
		panic(err)
	}

	hasDraft, err := doc.Has("draft")
	if err != nil {
		panic(err)
	}
	if hasDraft {
		_, _ = doc.Delete("draft")
	}

	out, err := doc.Bytes()
	if err != nil {
		panic(err)
	}

	fmt.Println(string(out))
}
```

## Update a File In Place

```go
err := mdfm.UpdateFile("note.md", func(doc *mdfm.Document) error {
	if err := doc.Set("title", "New Title"); err != nil {
		return err
	}
	_, err := doc.Delete("draft")
	return err
})

// You can also use the typed helper for common string keys.
err := mdfm.UpdateFile("note.md", func(doc *mdfm.Document) error {
	return doc.SetString("title", "New Title")
})
```

## Content-Level Mutation Helpers

Use `Mutate` or `MutateString` when your application already has markdown
content in memory.

```go
updated, changed, err := mdfm.MutateString(content, func(doc *mdfm.Document) error {
	if err := doc.SetString("title", "Updated"); err != nil {
		return err
	}

	hasDraft, err := doc.Has("draft")
	if err != nil {
		return err
	}
	if hasDraft {
		_, err = doc.Delete("draft")
		if err != nil {
			return err
		}
	}

	return nil
})
if err != nil {
	panic(err)
}
if changed {
	content = updated
}
```

## Downstream Integration Patterns

`mdfm` is intended to be the frontmatter manipulation engine for downstream
tools such as `mdid` and `mdfp`.

### Set If Missing (mdid-style)

```go
err := mdfm.UpdateFile(path, func(doc *mdfm.Document) error {
	hasUID, err := doc.Has("uid")
	if err != nil {
		return err
	}
	if hasUID {
		return nil
	}

	return doc.SetString("uid", generatedUID)
})
```

### Replace Metadata (mdfp-style)

```go
err := mdfm.UpdateFile(path, func(doc *mdfm.Document) error {
	return doc.SetString("fingerprint", newFingerprint)
})
```

### Remove Then Re-Add (mdfp-style)

```go
err := mdfm.UpdateFile(path, func(doc *mdfm.Document) error {
	_, err := doc.Delete("fingerprint")
	if err != nil {
		return err
	}

	return doc.SetString("fingerprint", newFingerprint)
})
```

Note: fingerprint calculation and UID generation stay in downstream tools.

## CLI

A small CLI is available in `cmd/mdfm`:

```bash
# set keys
mdfm --set title="My Note" --set draft=false note.md

# delete keys
mdfm --delete draft note.md
```

Note: CLI `--set` values are written as strings.

## Development

```bash
go test ./...
go test -bench=. -benchmem ./...
golangci-lint run ./...
```