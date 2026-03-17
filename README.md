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
- Frontmatter must be a YAML mapping for semantic key operations.

It is built to be fast, robust, and correct:

- Delimiter scanning is line-based and allocation-light.
- Frontmatter is parsed with `yaml.v3` for full YAML correctness.
- File updates are atomic and skip writes when content is unchanged.
- Symlink reads/updates are refused for safer file operations.

## Install

```bash
go get github.com/inful/mdfm
```

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
```

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