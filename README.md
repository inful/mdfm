# mdfm

`mdfm` is a small utility library for one purpose: reading, manipulating, and
updating YAML frontmatter in markdown files.

Scope:

- Frontmatter parsing, semantic key operations, serialization, and safe file updates.
- This package does not implement metadata business logic such as UID generation
	or fingerprint calculation.

## Operation Guarantees

- `Set` updates an existing top-level key in place, unless that key defines a
  YAML anchor something else aliases, in which case it returns `ErrAnchorInUse`.
- `Set` appends a missing top-level key.
- `Get` and `Has` return `found=false` for missing keys.
- `Delete` removes only the requested top-level key and reports whether it was removed.
- Repeating `Set` with the same value is idempotent at the byte level.
- Repeating `Delete` on a missing key is idempotent at the byte level.
- Newline style is preserved (`LF` vs `CRLF`) when parsing and mutating documents.
- A leading UTF-8 BOM is stripped so frontmatter written by a Windows editor is
  recognized, and written back on rewrite. A `...` closing delimiter is
  preserved too, so a no-op update leaves such files byte-identical.
- `Delete` and `Set` refuse a key that defines a YAML anchor another key still
  aliases, including an anchor defined inside a nested value, because writing
  the result would produce frontmatter that can no longer be parsed. The error
  is `ErrAnchorInUse`.
- A document whose first line is `---` is always read as opening a frontmatter
  block, so a markdown file that opens with a thematic break is rejected with
  `ErrUnclosedFrontmatter`. This matches Jekyll.
- Frontmatter must be a YAML mapping with unique mapping keys for semantic key operations.
- A document marker is only a marker at column 0. An indented `---` inside a
  value is value content, so a multi-line value containing one round-trips.
- A frontmatter block holding only comments is an empty mapping, not an error.
- Whitespace-only lines inside frontmatter are not preserved byte for byte, so
  an update can rewrite a file whose metadata did not change.

Key operations address a key by its scalar text. A key with no scalar value, such
as a sequence or mapping key (`? [1, 2]`), still parses and survives a rewrite
unchanged, but `Keys` reports it as `""`, `Get`, `Has`, and `Delete` cannot
match it, and `Frontmatter` fails to decode the mapping. Deleting one returns
`removed=false` with no error, so callers should not treat that as success.
Deleting a key that defines an anchor other keys alias returns `ErrAnchorInUse`
and changes nothing, because the result would no longer parse. Overwriting such
a key with `Set` is refused the same way. A
key written as a YAML scalar of another type, such as `1: one`, is addressable
by its text (`"1"`). An empty key is rejected by every operation that sets or
reads one.

Duplicate-key checking visits each YAML node once, so a frontmatter built from
many anchor aliases stays linear rather than growing with the number of paths
through the alias graph.

Byte-level idempotency covers `Set` followed by `Set`. It does not mean a parsed
document is preserved verbatim: serialization re-emits frontmatter through
`yaml.v3`, so quoting and formatting that `yaml.v3` considers redundant are
dropped (`title: "x"` becomes `title: x`).

A `Document` holds mutable state and must not be used from more than one
goroutine at a time. Separate `Document` values are safe to use concurrently:
the package keeps no mutable shared state.

It is built to be fast, robust, and correct:

- Delimiter scanning is line-based and allocation-light.
- Frontmatter is parsed with `yaml.v3` for full YAML correctness.
- Duplicate mapping keys are rejected during parse.
- File updates rewrite the existing inode and skip writes when content is
  unchanged. Extended attributes, ownership, file flags, inode identity and hard
  links therefore survive a write; the trade-off is that a hard kill mid-write
  can leave a partially written file, which no longer happens when updates are
  staged through a temp file and renamed.
- Only regular files are read or written. A FIFO, device, or socket is refused
  rather than opened, because opening one can block forever. `ReadFile` and
  `UpdateFile` refuse it; `WriteFile` refuses it too, but still creates a
  missing file.
- Symlink reads/updates are refused for safer file operations. The read also
  refuses to follow a final symlink at open time, so a symlink swapped in after
  the stat is caught too (on platforms with `O_NOFOLLOW`).

## Install

```bash
go get github.com/inful/mdfm
```

## Public API

Core parsing and mutation helpers:

- `Parse` and `ParseString` parse markdown content into a `Document`.
- `Mutate` and `MutateString` parse, mutate, and serialize content in one step.
- `ReadFile` parses a markdown file and refuses symlinks and non-regular files.
- `UpdateFile` mutates a markdown file in place and skips the write when bytes are unchanged.

`Document` accessors and mutation methods:

- `HasFrontmatter`, `Body`, `SetBody`, `Bytes`, and `WriteFile` work with whole-document state.
- `Frontmatter` and `SetFrontmatter` read or replace the full top-level frontmatter mapping.
- `Get`, `Has`, `GetString`, `Set`, `SetString`, `Delete`, and `Keys` provide top-level key operations.

Relevant exported errors:

- `ErrUnclosedFrontmatter` when an opening delimiter has no closing delimiter.
- `ErrFrontmatterNotMapping` when frontmatter is valid YAML but not a top-level mapping.
- `ErrDuplicateFrontmatterKey` when a mapping contains duplicate keys.
- `ErrEmptyKey` when a semantic key operation receives an empty key.
- `ErrAnchorInUse` when `Delete` or `Set` is asked to remove or replace a key
  that defines a YAML anchor other keys still alias.

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

Note: CLI `--set` values are written as strings. Only the key is trimmed; a
value's leading and trailing spaces are preserved. All `--set` flags are applied
before all `--delete` flags, regardless of the order given on the command line.
A key named in both flags is therefore removed: `mdfm --delete draft --set
draft=false note.md` ends with no `draft` key at all.

## Development

```bash
go test ./...
go test -bench=. -benchmem ./...
golangci-lint run ./...
```

`mise.toml` pins [gortex](https://github.com/zzet/gortex), an optional
code-graph explorer used while working on this library. Nothing in the build or
the test suite invokes it; `mise install` is only needed if you want it.

## License

GPL-3.0. See [LICENSE](LICENSE).