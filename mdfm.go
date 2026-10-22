// Package mdfm provides tools to parse, inspect, mutate, and persist YAML
// frontmatter in markdown files.
//
// The package models frontmatter as a top-level YAML mapping, preserves the
// document newline style when rewriting content, rejects duplicate mapping
// keys during parse, and refuses symlink-based file operations.
package mdfm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	openingDelimiter = "---"
	closingDelimiter = "---"
	altClosingMarker = "..."

	frontmatterKVPairWidth = 2
	bytesBufferSlack       = 64

	lf   = "\n"
	crlf = "\r\n"

	yamlStringTag = "!!str"
)

var (
	// utf8BOM is stripped before delimiter detection. Files written by some
	// Windows editors start with one, and without this the frontmatter is not
	// recognized, so the first Set prepends a second block and strands the
	// original metadata in the body.
	utf8BOM = []byte{0xEF, 0xBB, 0xBF}

	// ErrUnclosedFrontmatter is returned when an opening delimiter is present
	// without a closing delimiter.
	ErrUnclosedFrontmatter = errors.New("unclosed frontmatter block")

	// ErrFrontmatterNotMapping is returned when parsed frontmatter is valid YAML
	// but not a top-level mapping.
	ErrFrontmatterNotMapping = errors.New("frontmatter must be a YAML mapping")

	// ErrDuplicateFrontmatterKey is returned when frontmatter contains duplicate
	// mapping keys.
	ErrDuplicateFrontmatterKey = errors.New("frontmatter contains duplicate mapping keys")

	// ErrEmptyKey is returned when a frontmatter operation receives an empty key.
	ErrEmptyKey = errors.New("frontmatter key cannot be empty")

	// ErrAnchorInUse is returned when a key that defines a YAML anchor cannot be
	// deleted or overwritten because other keys still alias it. Doing it anyway
	// would write frontmatter that no longer parses.
	ErrAnchorInUse = errors.New("frontmatter anchor is still in use")

	errPathEmpty = errors.New("path cannot be empty")
)

// Document is a parsed markdown document with optional YAML frontmatter.
//
// A Document preserves the original markdown body and newline style. Its
// semantic frontmatter operations work only on top-level mapping keys.
type Document struct {
	hasFrontmatter bool
	frontmatter    yaml.Node
	body           []byte
	newline        string
	// hasBOM records that Parse stripped a leading UTF-8 BOM, so Bytes can put
	// it back. Dropping it would silently rewrite a file's encoding.
	hasBOM bool
	// closingMarker is the delimiter that closed the block as written, either
	// "---" or "...". Storing it keeps a document that used "..." byte-stable
	// across a rewrite.
	closingMarker string
}

// Parse parses markdown bytes into a Document.
//
// Frontmatter is recognized only when the document starts with an opening
// frontmatter delimiter on the first line. Parsed frontmatter must be a YAML
// mapping with unique mapping keys.
func Parse(content []byte) (*Document, error) {
	hasBOM := bytes.HasPrefix(content, utf8BOM)
	content = bytes.TrimPrefix(content, utf8BOM)

	line, next, newline, ok := scanLine(content, 0)
	if !ok || !isMarker(line, []byte(openingDelimiter)) {
		return &Document{
			hasFrontmatter: false,
			body:           slices.Clone(content),
			newline:        detectPreferredNewline(content),
			hasBOM:         hasBOM,
		}, nil
	}

	closeStart, closeEnd, marker, err := findClosingDelimiter(content, next)
	if err != nil {
		return nil, err
	}

	frontmatterRaw := content[next:closeStart]
	body := slices.Clone(content[closeEnd:])

	doc := &Document{
		hasFrontmatter: true,
		frontmatter:    emptyMappingNode(),
		body:           body,
		newline:        newline,
		hasBOM:         hasBOM,
		closingMarker:  marker,
	}

	if len(bytes.TrimSpace(frontmatterRaw)) == 0 {
		return doc, nil
	}

	parsedNode, err := parseFrontmatterMapping(frontmatterRaw)
	if err != nil {
		return nil, err
	}
	doc.frontmatter = parsedNode

	return doc, nil
}

// ParseString parses markdown text into a Document.
//
// It has the same parsing rules and error behavior as Parse.
func ParseString(content string) (*Document, error) {
	return Parse([]byte(content))
}

// Mutate parses markdown bytes, applies mutate, and serializes the document
// back to markdown.
//
// It returns the updated bytes, whether the serialized content changed, and any
// parse, mutation, or serialization error. A nil mutate callback is treated as
// a no-op.
//
// The input slice is never written to, so it can be compared against the result
// without copying it first.
func Mutate(content []byte, mutate func(*Document) error) ([]byte, bool, error) {
	doc, err := Parse(content)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse markdown: %w", err)
	}

	if mutate != nil {
		if err = mutate(doc); err != nil {
			return nil, false, fmt.Errorf("failed to mutate document: %w", err)
		}
	}

	updated, err := doc.Bytes()
	if err != nil {
		return nil, false, fmt.Errorf("failed to serialize document: %w", err)
	}

	return updated, !bytes.Equal(content, updated), nil
}

// MutateString parses markdown text, applies mutate, and serializes the
// document back to markdown text.
//
// It has the same behavior as Mutate but operates on strings.
func MutateString(content string, mutate func(*Document) error) (string, bool, error) {
	updated, changed, err := Mutate([]byte(content), mutate)
	if err != nil {
		return "", false, err
	}

	return string(updated), changed, nil
}

// ReadFile reads and parses a markdown file.
//
// Existing symlinks are refused. Parse errors are wrapped with additional file
// context.
func ReadFile(path string) (*Document, error) {
	if path == "" {
		return nil, errPathEmpty
	}

	content, _, err := readRegularFile(path)
	if err != nil {
		return nil, err
	}

	doc, err := Parse(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse markdown: %w", err)
	}

	return doc, nil
}

// UpdateFile reads a markdown file, applies mutate, and writes it back in place
// only when the serialized bytes change.
//
// The file is rewritten on its existing inode, so extended attributes, hard
// links and inode identity survive the write. Existing symlinks are refused. A
// nil mutate callback is treated as a no-op.
func UpdateFile(path string, mutate func(*Document) error) error {
	if path == "" {
		return errPathEmpty
	}

	original, perm, err := readRegularFile(path)
	if err != nil {
		return err
	}

	doc, err := Parse(original)
	if err != nil {
		return fmt.Errorf("failed to parse markdown: %w", err)
	}

	if mutate != nil {
		if err = mutate(doc); err != nil {
			return fmt.Errorf("failed to mutate document: %w", err)
		}
	}

	updated, err := doc.Bytes()
	if err != nil {
		return fmt.Errorf("failed to serialize document: %w", err)
	}

	if bytes.Equal(original, updated) {
		return nil
	}

	// The file exists, so O_CREATE leaves its mode alone. perm is still passed
	// so that a file removed between the read and the write is recreated
	// readable rather than as mode 0000. original is the undo for a write that
	// stops partway.
	if err = writeFileInPlace(path, updated, perm, original); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// HasFrontmatter reports whether the document has a frontmatter section.
//
// It returns false for a nil receiver.
func (d *Document) HasFrontmatter() bool {
	if d == nil {
		return false
	}
	return d.hasFrontmatter
}

// Body returns a copy of the markdown body.
//
// It returns nil for a nil receiver.
func (d *Document) Body() []byte {
	if d == nil {
		return nil
	}
	return slices.Clone(d.body)
}

// SetBody replaces the markdown body.
//
// The provided bytes are copied before they are stored. It is a no-op for a
// nil receiver.
func (d *Document) SetBody(body []byte) {
	if d == nil {
		return
	}
	d.body = slices.Clone(body)
}

// Frontmatter returns the full frontmatter as a decoded map.
//
// It returns an empty map when the document has no frontmatter or the receiver
// is nil.
func (d *Document) Frontmatter() (map[string]any, error) {
	if d == nil || !d.hasFrontmatter {
		return map[string]any{}, nil
	}

	if len(d.frontmatter.Content) == 0 {
		return map[string]any{}, nil
	}

	result := make(map[string]any, len(d.frontmatter.Content)/frontmatterKVPairWidth)
	if err := d.frontmatter.Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode frontmatter: %w", err)
	}

	return result, nil
}

// SetFrontmatter replaces the entire top-level frontmatter mapping.
//
// It creates frontmatter when needed and preserves the document newline style.
// It is a no-op for a nil receiver.
func (d *Document) SetFrontmatter(frontmatter map[string]any) error {
	if d == nil {
		return nil
	}

	// The parameter is typed map[string]any, so encoding it always yields a
	// mapping node and extractMappingNode can never fail here. A nil map
	// encodes as an empty mapping, not as a null scalar.
	node, err := encodeYAMLNode(frontmatter)
	if err != nil {
		return fmt.Errorf("failed to encode frontmatter: %w", err)
	}

	if err = validateMappingKeys(&node); err != nil {
		return err
	}

	// Struct-copy the mapping node rather than deep-cloning it. The Content
	// slice header in the copy still references the same heap-allocated array
	// as the original, which the Document now keeps alive through its slice
	// chain. yaml.v3's Encode already produced an independent snapshot of the
	// caller's map, so the Document is fully decoupled from it.
	d.frontmatter = node
	d.hasFrontmatter = true

	if d.newline == "" {
		d.newline = lf
	}

	return nil
}

// Get returns the decoded top-level frontmatter value stored at key.
//
// The returned boolean reports whether key was present. Missing keys and
// documents without frontmatter both return found=false. A nil receiver
// behaves like a document without frontmatter and returns found=false, nil.
func (d *Document) Get(key string) (any, bool, error) {
	if key == "" {
		return nil, false, ErrEmptyKey
	}
	if d == nil || !d.hasFrontmatter {
		return nil, false, nil
	}

	valueNode, ok := d.valueNodeForKey(key)
	if !ok {
		return nil, false, nil
	}

	var value any
	if err := valueNode.Decode(&value); err != nil {
		return nil, false, fmt.Errorf("failed to decode value for key %q: %w", key, err)
	}

	return value, true, nil
}

// Has reports whether a top-level frontmatter key exists.
//
// Documents without frontmatter and a nil receiver both return false, nil.
func (d *Document) Has(key string) (bool, error) {
	if key == "" {
		return false, ErrEmptyKey
	}
	if d == nil || !d.hasFrontmatter {
		return false, nil
	}

	_, ok := d.valueNodeForKey(key)

	return ok, nil
}

// GetString returns the string value stored at a top-level frontmatter key.
//
// It returns found=true with an error when the key exists but does not decode
// to a Go string. A nil receiver behaves like a document without frontmatter
// and returns found=false, nil.
func (d *Document) GetString(key string) (string, bool, error) {
	value, ok, err := d.Get(key)
	if err != nil || !ok {
		return "", ok, err
	}

	stringValue, typeOK := value.(string)
	if !typeOK {
		return "", true, fmt.Errorf("value for key %q is %T, not string", key, value)
	}

	return stringValue, true, nil
}

// SetString sets or adds a top-level frontmatter string key/value pair.
//
// It is Set with a string value and reports the same errors, including
// ErrAnchorInUse.
func (d *Document) SetString(key, value string) error {
	return d.Set(key, value)
}

// Set sets or adds a top-level frontmatter key/value pair.
//
// It creates an empty frontmatter mapping when the document does not already
// have one. It is a no-op for a nil receiver.
//
// Replacing an existing key returns ErrAnchorInUse when that key defines a
// YAML anchor another key still aliases, because the result would be
// frontmatter that no longer parses.
func (d *Document) Set(key string, value any) error {
	if key == "" {
		return ErrEmptyKey
	}
	if d == nil {
		return nil
	}

	d.ensureFrontmatter()

	valueNode, err := nodeFromValue(value)
	if err != nil {
		return err
	}

	return d.upsertValueNode(key, valueNode)
}

// Delete removes a top-level frontmatter key.
//
// It reports whether the key was removed. Documents without frontmatter and a
// nil receiver both return false, nil.
func (d *Document) Delete(key string) (bool, error) {
	if key == "" {
		return false, ErrEmptyKey
	}
	if d == nil || !d.hasFrontmatter {
		return false, nil
	}

	idx := d.findKeyIndex(key)
	if idx < 0 {
		return false, nil
	}

	// Dropping this pair can orphan an anchor it defines, leaving dangling
	// aliases that no longer parse.
	if err := d.checkAnchorsRemain(idx); err != nil {
		return false, err
	}

	// slices.Delete clears the vacated tail slots; a plain append-shift would
	// leave the deleted key and value reachable through the backing array,
	// pinning their memory until the next realloc.
	d.frontmatter.Content = slices.Delete(d.frontmatter.Content, idx, idx+frontmatterKVPairWidth)

	return true, nil
}

// checkAnchorsRemain reports an error when removing or replacing the key/value
// pair at idx would drop an anchor the rest of the document still aliases. Both
// nodes of the pair are scanned, because YAML lets the anchor sit on the key
// (`&b base: v`) as well as the value. The pair itself is left out of the alias
// search: an anchor and its only alias inside one removed subtree go away
// together, and that is safe.
func (d *Document) checkAnchorsRemain(idx int) error {
	// The pair at idx goes away with the mutation, so an alias inside it does
	// not keep its own anchor alive.
	rest := slices.Concat(
		d.frontmatter.Content[:idx],
		d.frontmatter.Content[idx+frontmatterKVPairWidth:],
	)

	seen := make(map[string]struct{})
	var orphan string

	var scan func(*yaml.Node)
	scan = func(node *yaml.Node) {
		if node == nil || orphan != "" {
			return
		}
		if _, done := seen[node.Anchor]; !done && node.Anchor != "" {
			seen[node.Anchor] = struct{}{}
			if aliasesAnchor(rest, node.Anchor) {
				orphan = node.Anchor
			}
		}
		for _, child := range node.Content {
			scan(child)
		}
	}

	for _, node := range d.frontmatter.Content[idx : idx+frontmatterKVPairWidth] {
		scan(node)
	}

	if orphan != "" {
		return fmt.Errorf("%w: %q defines anchor %q", ErrAnchorInUse, d.frontmatter.Content[idx].Value, orphan)
	}

	return nil
}

// aliasesAnchor reports whether any node references the named anchor. Only
// alias nodes are matched, never their target, so a document whose aliases
// point at each other cannot make this walk cyclic.
func aliasesAnchor(nodes []*yaml.Node, anchor string) bool {
	for _, node := range nodes {
		// A parsed alias carries the anchor name in Value, whether it stands
		// alone, sits in a sequence or is nested in a mapping. Value is
		// sufficient; Alias.Anchor always agrees with it.
		if node.Kind == yaml.AliasNode && node.Value == anchor {
			return true
		}
		if aliasesAnchor(node.Content, anchor) {
			return true
		}
	}

	return false
}

// Keys returns top-level frontmatter keys in document order.
//
// It returns nil, nil when the document has no frontmatter or the receiver is
// nil.
func (d *Document) Keys() ([]string, error) {
	if d == nil || !d.hasFrontmatter {
		return nil, nil
	}

	keys := make([]string, 0, len(d.frontmatter.Content)/frontmatterKVPairWidth)
	for i := 0; i < len(d.frontmatter.Content); i += frontmatterKVPairWidth {
		keys = append(keys, d.frontmatter.Content[i].Value)
	}

	return keys, nil
}

// Bytes serializes the document back to markdown bytes.
//
// Documents without frontmatter serialize to a copy of the body. A nil receiver
// returns nil, nil.
func (d *Document) Bytes() ([]byte, error) {
	if d == nil {
		return nil, nil
	}
	if !d.hasFrontmatter {
		return d.withBOM(slices.Clone(d.body)), nil
	}

	newline := d.newline
	if newline == "" {
		newline = lf
	}

	marker := d.closingMarker
	if marker == "" {
		marker = closingDelimiter
	}

	var buf bytes.Buffer
	buf.Grow(len(d.body) + bytesBufferSlack + len(utf8BOM))

	if d.hasBOM {
		buf.Write(utf8BOM)
	}

	buf.WriteString(openingDelimiter)
	buf.WriteString(newline)

	if len(d.frontmatter.Content) > 0 {
		// yaml.Marshal always emits LF line endings, so normalize only after
		// marshaling to preserve the document's preferred newline style.
		frontmatterBytes, err := yaml.Marshal(&d.frontmatter)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal frontmatter: %w", err)
		}

		if newline == crlf {
			frontmatterBytes = bytes.ReplaceAll(frontmatterBytes, []byte(lf), []byte(crlf))
		}

		// yaml.Marshal always terminates with a newline, and the CRLF rewrite
		// above turns that into the document's style, so no separator is needed.
		buf.Write(frontmatterBytes)
	}

	buf.WriteString(marker)
	buf.WriteString(newline)
	buf.Write(d.body)

	return buf.Bytes(), nil
}

// withBOM re-emits a leading UTF-8 BOM when Parse stripped one, so a rewrite
// does not silently change a file's encoding.
func (d *Document) withBOM(content []byte) []byte {
	if !d.hasBOM {
		return content
	}

	out := make([]byte, 0, len(utf8BOM)+len(content))
	out = append(out, utf8BOM...)

	return append(out, content...)
}

// WriteFile serializes the document and writes it in place, creating the file
// with perm when it does not exist yet.
//
// The file is rewritten on its existing inode, so extended attributes, hard
// links and inode identity survive the write. Existing symlinks are refused. It
// is a no-op for a nil receiver.
func (d *Document) WriteFile(path string, perm os.FileMode) error {
	if d == nil {
		return nil
	}
	if path == "" {
		return errPathEmpty
	}

	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write symlink: %s", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to write non-regular file: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	data, err := d.Bytes()
	if err != nil {
		return fmt.Errorf("failed to serialize document: %w", err)
	}

	if err = writeFileInPlace(path, data, perm, nil); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (d *Document) ensureFrontmatter() {
	if d.hasFrontmatter {
		return
	}
	d.hasFrontmatter = true
	d.frontmatter = emptyMappingNode()
}

func (d *Document) findKeyIndex(key string) int {
	for i := 0; i < len(d.frontmatter.Content); i += frontmatterKVPairWidth {
		if d.frontmatter.Content[i].Value == key {
			return i
		}
	}

	return -1
}

func (d *Document) valueNodeForKey(key string) (*yaml.Node, bool) {
	idx := d.findKeyIndex(key)
	if idx < 0 {
		return nil, false
	}

	return d.frontmatter.Content[idx+1], true
}

// upsertValueNode replaces the value at key or appends it. Replacing a value
// that defines an anchor would orphan every alias to it, so it is refused the
// same way Delete refuses.
func (d *Document) upsertValueNode(key string, valueNode *yaml.Node) error {
	idx := d.findKeyIndex(key)
	if idx >= 0 {
		if err := d.checkAnchorsRemain(idx); err != nil {
			return err
		}
		d.frontmatter.Content[idx+1] = valueNode
		return nil
	}

	d.frontmatter.Content = append(
		d.frontmatter.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: yamlStringTag, Value: key},
		valueNode,
	)

	return nil
}

func parseFrontmatterMapping(data []byte) (yaml.Node, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return yaml.Node{}, fmt.Errorf("failed to parse YAML frontmatter: %w", err)
	}

	mappingNode, err := extractMappingNode(&root)
	if err != nil {
		return yaml.Node{}, err
	}
	// Reject duplicate keys before the document escapes into the public API so
	// every accessor sees the same unambiguous data model. A YAML anchor can be
	// aliased any number of times and each alias re-walks its whole target
	// subtree, so a chain of anchors costs 2^depth that way. The visited set
	// keeps it linear; the check is a pure predicate, so skipping a repeat
	// cannot change the answer.
	if err := validateUniqueMappingKeysVisited(mappingNode, make(map[*yaml.Node]struct{})); err != nil {
		return yaml.Node{}, err
	}

	// Return a struct copy of the mapping node. The Content slice header in
	// the copy still references the same heap-allocated array as the original
	// (so the children remain reachable through the Document's slice chain),
	// avoiding a deep clone of every node in the frontmatter.
	return *mappingNode, nil
}

// A Go map can carry an empty-string key, which no other operation could then
// address: Get, Has, and Delete all reject an empty key, so the value would be
// written to the user's file and be impossible to remove. Complex (non-scalar)
// keys are left alone; they also have an empty Value, but they are documented
// as unreachable and must still round-trip.
//
// Only the top level is checked. Nested keys are never addressable by Get, Has,
// or Delete regardless of their text.
func validateMappingKeys(mapping *yaml.Node) error {
	for i := 0; i < len(mapping.Content); i += frontmatterKVPairWidth {
		keyNode := mapping.Content[i]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == "" {
			return ErrEmptyKey
		}
	}

	return nil
}

// A YAML anchor can be aliased any number of times, and each alias re-walks
// its whole target subtree. A chain of anchors where each aliases its
// predecessor twice costs 2^depth that way, so an 800-byte frontmatter can
// burn minutes of CPU. Validating each node at most once keeps it linear;
// the check is a pure predicate, so skipping a repeat cannot change the
// answer.
func validateUniqueMappingKeysVisited(node *yaml.Node, visited map[*yaml.Node]struct{}) error {
	if _, done := visited[node]; done {
		return nil
	}
	visited[node] = struct{}{}

	switch node.Kind {
	case yaml.MappingNode:
		return validateUniqueKeysInMapping(node, visited)
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if err := validateUniqueMappingKeysVisited(child, visited); err != nil {
				return err
			}
		}
		return nil
	case yaml.AliasNode:
		return validateUniqueMappingKeysVisited(node.Alias, visited)
	}

	return nil
}

func validateUniqueKeysInMapping(node *yaml.Node, visited map[*yaml.Node]struct{}) error {
	seen := make(map[string]struct{}, len(node.Content)/frontmatterKVPairWidth)
	for i := 0; i < len(node.Content); i += frontmatterKVPairWidth {
		keyNode := node.Content[i]
		keyID, err := mappingKeyIdentity(keyNode)
		if err != nil {
			return err
		}
		if _, exists := seen[keyID]; exists {
			return fmt.Errorf("%w: %s", ErrDuplicateFrontmatterKey, mappingKeyLabel(keyNode))
		}
		seen[keyID] = struct{}{}

		if err := validateUniqueMappingKeysVisited(node.Content[i+1], visited); err != nil {
			return err
		}
	}

	return nil
}

func mappingKeyIdentity(node *yaml.Node) (string, error) {
	// Scalar keys are the overwhelming common case. The Value is already a
	// canonical string for equality; yaml.Marshal would re-encode it
	// (allocating a new emitter, buffer, and string each time) for no
	// benefit. Complex keys fall back to Marshal.
	//
	// shortcut: an alias key and the anchor it points at marshal to
	// different text ("*a" vs the inline mapping), so they are not seen as
	// duplicates. Resolve aliases before Marshal to compare by target if a
	// real file ever needs that distinction.
	if node.Kind == yaml.ScalarNode {
		return node.Value, nil
	}

	encoded, err := yaml.Marshal(node)
	if err != nil {
		return "", fmt.Errorf("failed to encode frontmatter key: %w", err)
	}

	return string(encoded), nil
}

func mappingKeyLabel(node *yaml.Node) string {
	identity, err := mappingKeyIdentity(node)
	if err != nil {
		return "<unprintable key>"
	}

	return strings.TrimSpace(identity)
}

func extractMappingNode(root *yaml.Node) (*yaml.Node, error) {
	node := root

	// yaml.v3 leaves Kind at its zero value for an empty document: blank
	// input, whitespace only, or comments only. That is an empty mapping, not
	// a document that is some other shape.
	if node.Kind == 0 {
		empty := emptyMappingNode()
		return &empty, nil
	}

	// yaml.Unmarshal always wraps the value in a DocumentNode with one child.
	if node.Kind == yaml.DocumentNode {
		node = node.Content[0]
	}

	if node.Kind != yaml.MappingNode {
		return nil, ErrFrontmatterNotMapping
	}

	return node, nil
}

func nodeFromValue(value any) (*yaml.Node, error) {
	node, err := encodeYAMLNode(value)
	if err != nil {
		return nil, fmt.Errorf("failed to encode value: %w", err)
	}

	return &node, nil
}

// encodeYAMLNode encodes v into a yaml.Node. yaml.v3 panics rather than
// returning an error for types it cannot encode (functions, channels, unsafe
// pointers); this helper recovers that panic so public callers can rely on
// the normal error-return contract.
//
// shortcut: the recover catches every panic from yaml.v3, not only the
// unencodable-type one, and yaml.v3 exports no sentinel to narrow it to. The
// panic payload is an unexported yamlError type. Replace with an explicit
// reflect.Kind allowlist check if a yaml.v3 internal panic ever needs to
// surface as a crash instead of an error.
func encodeYAMLNode(v any) (node yaml.Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()

	if encodeErr := node.Encode(v); encodeErr != nil {
		return yaml.Node{}, encodeErr
	}
	return node, nil
}

func findClosingDelimiter(content []byte, start int) (closeStart int, closeEnd int, marker string, err error) {
	for pos := start; pos <= len(content); {
		line, next, _, ok := scanLine(content, pos)
		if !ok {
			break
		}

		if marker = closingMarkerFor(line); marker != "" {
			return pos, next, marker, nil
		}

		// scanLine only reports ok when start < len(content), so next is
		// always greater than pos here; no progress check is needed.
		pos = next
	}

	return 0, 0, "", ErrUnclosedFrontmatter
}

// closingMarkerFor returns the delimiter line is, or "" when it is not one.
func closingMarkerFor(line []byte) string {
	if isMarker(line, []byte(closingDelimiter)) {
		return closingDelimiter
	}
	if isMarker(line, []byte(altClosingMarker)) {
		return altClosingMarker
	}

	return ""
}

func scanLine(content []byte, start int) (line []byte, next int, newline string, ok bool) {
	if start > len(content) {
		return nil, 0, "", false
	}
	if start == len(content) {
		return nil, start, "", false
	}

	for i := start; i < len(content); i++ {
		if content[i] == '\n' {
			if i > start && content[i-1] == '\r' {
				return content[start : i-1], i + 1, crlf, true
			}
			return content[start:i], i + 1, lf, true
		}
	}

	return content[start:], len(content), "", true
}

// A YAML document marker is only a marker at column 0. yaml.v3 indents
// block-scalar content by four spaces, so accepting an indented "---" cut a
// multi-line value in half and spliced the remainder into the body. Only
// trailing whitespace after the marker is allowed.
func isMarker(line, marker []byte) bool {
	return bytes.Equal(bytes.TrimRight(line, " \t\r"), marker)
}

func detectPreferredNewline(content []byte) string {
	crlfCount := bytes.Count(content, []byte(crlf))
	if crlfCount == 0 {
		return lf
	}

	// bytes.Contains would let a single stray CRLF inside an LF file flip the
	// whole new frontmatter block, so require CRLF to be the majority style.
	lfCount := bytes.Count(content, []byte(lf)) - crlfCount
	if crlfCount > lfCount {
		return crlf
	}
	return lf
}

func emptyMappingNode() yaml.Node {
	return yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

// readRegularFile reads a path that must be a plain regular file, refusing a
// symlink and refusing anything that would block forever on open. ReadFile and
// UpdateFile share it so the two entry points cannot drift apart. The mode
// comes back so a rewrite that has to recreate the file does not make it
// unreadable.
func readRegularFile(path string) ([]byte, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to stat file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, fmt.Errorf("refusing to read symlink: %s", path)
	}
	// Opening a FIFO or device node can block forever, and the mode is
	// already in hand.
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("refusing to read non-regular file: %s", path)
	}

	content, err := readFileNoFollow(path)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read file: %w", err)
	}

	return content, info.Mode().Perm(), nil
}

// readFileNoFollow reads path, refusing a final symlink component at open time
// rather than relying on an earlier Lstat that an attacker could invalidate.
func readFileNoFollow(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|noFollow, 0) //nolint:gosec // path is caller-supplied by design.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	// shortcut: three copies of the file are live at once during an update:
	// this read buffer, the body Parse clones, and the buffer Bytes builds.
	// Measured at roughly 3x file size, which is 98MB for a 32MB markdown file.
	// That is fine for notes, which are kilobytes. Pre-size the read loop
	// (os.ReadFile's, plus a size+1 so the last read does not force a realloc)
	// and drop the body clone if this library ever handles large files in bulk.
	return io.ReadAll(file)
}

// writeFileInPlace writes data over the inode already at path rather than
// renaming a temporary file into place. Renaming replaced the directory entry
// with a fresh inode, which silently dropped every attribute of the original:
// extended attributes, ownership, ACLs and file flags were discarded, and a
// file with hard links was split so the other names kept stale content.
// Writing the existing inode keeps all of them, and keeps the inode identity
// that watchers and indexers key on.
//
// shortcut: a crash between the write and the truncate can leave a partially
// written file. Replace with a write-temp-then-copy-into-inode scheme if
// crash-atomic frontmatter edits are ever required.
// writeFileInPlace rewrites path on its own inode, so extended attributes,
// hard links and file identity survive. restore is the file's previous content
// and may be nil; it is written back when the new content cannot be written in
// full, because a write that stops partway leaves the new prefix in front of
// the old tail.
// restoreFile puts path back to the bytes it held before a failed write. It
// rewrites in place, so the inode, its links and its extended attributes are
// the same ones the failed write was aimed at.
func restoreFile(path string, original []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|noFollow, 0) //nolint:gosec // path is caller-supplied by design.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	if _, err = file.WriteAt(original, 0); err != nil {
		return err
	}

	return file.Truncate(int64(len(original)))
}

func writeFileInPlace(path string, data []byte, perm os.FileMode, restore []byte) error {
	// O_CREATE leaves an existing file's mode alone and applies perm only to a
	// newly created one; noFollow refuses a final symlink component, matching
	// the read path.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|noFollow, perm) //nolint:gosec // path is caller-supplied by design.
	if err != nil {
		return err
	}

	// Write before truncating so a partially failed write leaves the file at its
	// original length rather than a short one. A write that stops partway
	// instead of failing outright would leave the new prefix in front of the
	// old tail, which parses as frontmatter but says something different, so
	// that case puts the original bytes back. No test can reach either branch:
	// it needs a write that fails partway, and the only fault injector
	// available (/dev/full, Linux-only, and ulimit -f elsewhere) fails
	// outright. Treat this as a judgement call, not a verified guarantee.
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		if restore != nil {
			_ = restoreFile(path, restore)
		}
		return err
	}
	if err = file.Truncate(int64(len(data))); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}
