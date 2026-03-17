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
	"os"
	"path/filepath"
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
)

var (
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
}

// Parse parses markdown bytes into a Document.
//
// Frontmatter is recognized only when the document starts with an opening
// frontmatter delimiter on the first line. Parsed frontmatter must be a YAML
// mapping with unique mapping keys.
func Parse(content []byte) (*Document, error) {
	line, next, newline, ok := scanLine(content, 0)
	if !ok || !isOpeningDelimiter(line) {
		return &Document{
			hasFrontmatter: false,
			body:           slices.Clone(content),
			newline:        detectPreferredNewline(content),
		}, nil
	}

	closeStart, closeEnd, err := findClosingDelimiter(content, next)
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
func Mutate(content []byte, mutate func(*Document) error) ([]byte, bool, error) {
	original := slices.Clone(content)

	doc, err := Parse(content)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse markdown: %w", err)
	}

	if err = applyMutation(doc, mutate); err != nil {
		return nil, false, err
	}

	updated, err := doc.Bytes()
	if err != nil {
		return nil, false, fmt.Errorf("failed to serialize document: %w", err)
	}

	return updated, !bytes.Equal(original, updated), nil
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

	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to read symlink: %s", path)
	}

	content, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	doc, err := Parse(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse markdown: %w", err)
	}

	return doc, nil
}

// UpdateFile reads a markdown file, applies mutate, and writes it back
// atomically only when the serialized bytes change.
//
// Existing symlinks are refused. A nil mutate callback is treated as a no-op.
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

	if err = applyMutation(doc, mutate); err != nil {
		return err
	}

	updated, err := doc.Bytes()
	if err != nil {
		return fmt.Errorf("failed to serialize document: %w", err)
	}

	if bytes.Equal(original, updated) {
		return nil
	}

	if err = writeFileAtomic(path, updated, perm); err != nil {
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

	if d.frontmatter.Kind != yaml.MappingNode {
		return nil, ErrFrontmatterNotMapping
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

	var node yaml.Node
	if err := node.Encode(frontmatter); err != nil {
		return fmt.Errorf("failed to encode frontmatter: %w", err)
	}

	mappingNode, err := extractMappingNode(&node)
	if err != nil {
		return err
	}

	d.frontmatter = cloneNode(mappingNode)
	d.hasFrontmatter = true

	if d.newline == "" {
		d.newline = lf
	}

	return nil
}

// Get returns the decoded top-level frontmatter value stored at key.
//
// The returned boolean reports whether key was present. Missing keys and
// documents without frontmatter both return found=false.
func (d *Document) Get(key string) (any, bool, error) {
	if key == "" {
		return nil, false, ErrEmptyKey
	}
	if d == nil || !d.hasFrontmatter {
		return nil, false, nil
	}

	valueNode, ok, err := d.valueNodeForKey(key)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}

	var value any
	if err = valueNode.Decode(&value); err != nil {
		return nil, false, fmt.Errorf("failed to decode value for key %q: %w", key, err)
	}

	return value, true, nil
}

// Has reports whether a top-level frontmatter key exists.
//
// Documents without frontmatter return false, nil.
func (d *Document) Has(key string) (bool, error) {
	if key == "" {
		return false, ErrEmptyKey
	}
	if d == nil || !d.hasFrontmatter {
		return false, nil
	}

	_, ok, err := d.valueNodeForKey(key)
	if err != nil {
		return false, err
	}

	return ok, nil
}

// GetString returns the string value stored at a top-level frontmatter key.
//
// It returns found=true with an error when the key exists but does not decode
// to a Go string.
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
func (d *Document) SetString(key, value string) error {
	return d.Set(key, value)
}

// Set sets or adds a top-level frontmatter key/value pair.
//
// It creates an empty frontmatter mapping when the document does not already
// have one. It is a no-op for a nil receiver.
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
// It reports whether the key was removed. Documents without frontmatter return
// false, nil.
func (d *Document) Delete(key string) (bool, error) {
	if key == "" {
		return false, ErrEmptyKey
	}
	if d == nil || !d.hasFrontmatter {
		return false, nil
	}

	idx, err := d.findKeyIndex(key)
	if err != nil {
		return false, err
	}
	if idx < 0 {
		return false, nil
	}

	d.deleteValueAtIndex(idx)

	return true, nil
}

// Keys returns top-level frontmatter keys in document order.
//
// It returns nil, nil when the document has no frontmatter or the receiver is
// nil.
func (d *Document) Keys() ([]string, error) {
	if d == nil || !d.hasFrontmatter {
		return nil, nil
	}
	if d.frontmatter.Kind != yaml.MappingNode {
		return nil, ErrFrontmatterNotMapping
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
		return slices.Clone(d.body), nil
	}

	newline := d.newline
	if newline == "" {
		newline = lf
	}

	var buf bytes.Buffer
	buf.Grow(len(d.body) + bytesBufferSlack)

	buf.WriteString(openingDelimiter)
	buf.WriteString(newline)

	if len(d.frontmatter.Content) > 0 {
		// yaml.Marshal always emits LF line endings, so normalize only after
		// marshaling to preserve the document's preferred newline style.
		frontmatterBytes, err := marshalFrontmatter(&d.frontmatter)
		if err != nil {
			return nil, err
		}

		if newline == crlf {
			frontmatterBytes = bytes.ReplaceAll(frontmatterBytes, []byte(lf), []byte(crlf))
		}

		buf.Write(frontmatterBytes)
		if !bytes.HasSuffix(frontmatterBytes, []byte(newline)) {
			buf.WriteString(newline)
		}
	}

	buf.WriteString(closingDelimiter)
	buf.WriteString(newline)
	buf.Write(d.body)

	return buf.Bytes(), nil
}

// WriteFile serializes the document and writes it atomically with perm.
//
// Existing symlinks are refused. It is a no-op for a nil receiver.
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
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	data, err := d.Bytes()
	if err != nil {
		return fmt.Errorf("failed to serialize document: %w", err)
	}

	if err = writeFileAtomic(path, data, perm); err != nil {
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
	if d.newline == "" {
		d.newline = lf
	}
}

func (d *Document) findKeyIndex(key string) (int, error) {
	if d.frontmatter.Kind != yaml.MappingNode {
		return -1, ErrFrontmatterNotMapping
	}

	for i := 0; i < len(d.frontmatter.Content); i += frontmatterKVPairWidth {
		if d.frontmatter.Content[i].Value == key {
			return i, nil
		}
	}

	return -1, nil
}

func (d *Document) valueNodeForKey(key string) (*yaml.Node, bool, error) {
	idx, err := d.findKeyIndex(key)
	if err != nil {
		return nil, false, err
	}
	if idx < 0 {
		return nil, false, nil
	}

	return d.frontmatter.Content[idx+1], true, nil
}

func (d *Document) upsertValueNode(key string, valueNode *yaml.Node) error {
	idx, err := d.findKeyIndex(key)
	if err != nil {
		return err
	}
	if idx >= 0 {
		d.frontmatter.Content[idx+1] = valueNode
		return nil
	}

	d.appendKeyValueNode(key, valueNode)
	return nil
}

func (d *Document) appendKeyValueNode(key string, valueNode *yaml.Node) {
	d.frontmatter.Content = append(
		d.frontmatter.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		valueNode,
	)
}

func (d *Document) deleteValueAtIndex(idx int) {
	d.frontmatter.Content = append(
		d.frontmatter.Content[:idx],
		d.frontmatter.Content[idx+2:]...,
	)
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
	// every accessor sees the same unambiguous data model.
	if err := validateUniqueMappingKeys(mappingNode); err != nil {
		return yaml.Node{}, err
	}

	return cloneNode(mappingNode), nil
}

func validateUniqueMappingKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case yaml.MappingNode:
		return validateUniqueKeysInMapping(node)
	case yaml.SequenceNode:
		return validateUniqueKeysInChildren(node.Content)
	case yaml.AliasNode:
		if node.Alias != nil {
			return validateUniqueMappingKeys(node.Alias)
		}
	}

	return nil
}

func validateUniqueKeysInMapping(node *yaml.Node) error {
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

		if err := validateUniqueMappingKeys(node.Content[i+1]); err != nil {
			return err
		}
	}

	return nil
}

func validateUniqueKeysInChildren(children []*yaml.Node) error {
	for _, child := range children {
		if err := validateUniqueMappingKeys(child); err != nil {
			return err
		}
	}

	return nil
}

func mappingKeyIdentity(node *yaml.Node) (string, error) {
	if node == nil {
		return "", fmt.Errorf("%w: <nil>", ErrDuplicateFrontmatterKey)
	}

	encoded, err := yaml.Marshal(node)
	if err != nil {
		return "", fmt.Errorf("failed to encode frontmatter key: %w", err)
	}

	return string(encoded), nil
}

func mappingKeyLabel(node *yaml.Node) string {
	if node == nil {
		return "<nil>"
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}

	encoded, err := yaml.Marshal(node)
	if err != nil {
		return "<unprintable key>"
	}

	return strings.TrimSpace(string(encoded))
}

func extractMappingNode(root *yaml.Node) (*yaml.Node, error) {
	if root == nil {
		return nil, ErrFrontmatterNotMapping
	}

	node := root
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			empty := emptyMappingNode()
			return &empty, nil
		}
		node = node.Content[0]
	}

	if node.Kind != yaml.MappingNode {
		return nil, ErrFrontmatterNotMapping
	}

	return node, nil
}

func nodeFromValue(value any) (*yaml.Node, error) {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return nil, fmt.Errorf("failed to encode value: %w", err)
	}

	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			empty := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
			return empty, nil
		}
		return cloneNodePtr(node.Content[0]), nil
	}

	return &node, nil
}

func marshalFrontmatter(mapping *yaml.Node) ([]byte, error) {
	if mapping == nil {
		return nil, nil
	}
	if mapping.Kind != yaml.MappingNode {
		return nil, ErrFrontmatterNotMapping
	}

	bytesOut, err := yaml.Marshal(mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal frontmatter: %w", err)
	}

	return bytesOut, nil
}

func findClosingDelimiter(content []byte, start int) (closeStart int, closeEnd int, err error) {
	for pos := start; pos <= len(content); {
		line, next, _, ok := scanLine(content, pos)
		if !ok {
			break
		}

		if isClosingDelimiter(line) {
			return pos, next, nil
		}

		if next == pos {
			break
		}
		pos = next
	}

	return 0, 0, ErrUnclosedFrontmatter
}

func scanLine(content []byte, start int) (line []byte, next int, newline string, ok bool) {
	if start < 0 || start > len(content) {
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

func isOpeningDelimiter(line []byte) bool {
	return string(bytes.TrimSpace(line)) == openingDelimiter
}

func isClosingDelimiter(line []byte) bool {
	trimmed := string(bytes.TrimSpace(line))
	return trimmed == closingDelimiter || trimmed == altClosingMarker
}

func detectPreferredNewline(content []byte) string {
	if bytes.Contains(content, []byte(crlf)) {
		return crlf
	}
	return lf
}

func emptyMappingNode() yaml.Node {
	return yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func cloneNodePtr(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	cloned := cloneNode(node)
	return &cloned
}

func cloneNode(node *yaml.Node) yaml.Node {
	if node == nil {
		return yaml.Node{}
	}

	cloned := *node
	if len(node.Content) == 0 {
		cloned.Content = nil
		return cloned
	}

	cloned.Content = make([]*yaml.Node, len(node.Content))
	for i := range node.Content {
		child := cloneNode(node.Content[i])
		cloned.Content[i] = &child
	}

	return cloned
}

func readRegularFile(path string) ([]byte, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to stat file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, fmt.Errorf("refusing to update symlink: %s", path)
	}

	content, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read file: %w", err)
	}

	return content, info.Mode().Perm(), nil
}

func applyMutation(doc *Document, mutate func(*Document) error) error {
	if mutate == nil {
		return nil
	}

	if err := mutate(doc); err != nil {
		return fmt.Errorf("failed to mutate document: %w", err)
	}

	return nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) (retErr error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".mdfm-*")
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			_ = os.Remove(tmp.Name())
		}
	}()

	// Write and sync the temporary file before renaming so the destination is
	// replaced atomically with fully flushed contents.
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}

	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}

	return nil
}
