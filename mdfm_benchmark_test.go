package mdfm

import (
	"fmt"
	"testing"
)

var benchmarkDocument = []byte(`---
title: Example
tags:
  - go
  - markdown
draft: false
count: 42
---
# Heading

This is a benchmark payload.
`)

var benchmarkLargeDocument = []byte(`---
title: Large Frontmatter Document
author: Test Author
date: 2026-06-17
tags:
  - go
  - markdown
  - yaml
  - frontmatter
  - library
  - parser
  - performance
  - optimization
  - testing
  - benchmark
description: A large frontmatter document for performance testing
category: benchmark
status: active
version: 1.0.0
license: MIT
homepage: https://example.com
repository: https://github.com/example/repo
keywords:
  - performance
  - parsing
  - yaml
  - go
contributors:
  - name: Alice
    email: alice@example.com
  - name: Bob
    email: bob@example.com
  - name: Charlie
    email: charlie@example.com
---
# Large Document

This is a benchmark payload with lots of frontmatter.
`)

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := Parse(benchmarkDocument); err != nil {
			b.Fatalf("Parse returned error: %v", err)
		}
	}
}

func BenchmarkParseLarge(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := Parse(benchmarkLargeDocument); err != nil {
			b.Fatalf("Parse returned error: %v", err)
		}
	}
}

func BenchmarkSetAndBytes(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	index := 0
	for b.Loop() {
		doc, err := Parse(benchmarkDocument)
		if err != nil {
			b.Fatalf("Parse returned error: %v", err)
		}

		if err = doc.Set("index", index); err != nil {
			b.Fatalf("Set returned error: %v", err)
		}

		if _, err = doc.Bytes(); err != nil {
			b.Fatalf("Bytes returned error: %v", err)
		}

		index++
	}
}

// BenchmarkBytesOnly isolates the cost of serializing a parsed document.
func BenchmarkBytesOnly(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	doc, err := Parse(benchmarkDocument)
	if err != nil {
		b.Fatalf("Parse returned error: %v", err)
	}

	for b.Loop() {
		if _, err = doc.Bytes(); err != nil {
			b.Fatalf("Bytes returned error: %v", err)
		}
	}
}

// BenchmarkSetMany exercises the Set + findKeyIndex path with many keys.
func BenchmarkSetMany(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		doc, err := Parse(benchmarkDocument)
		if err != nil {
			b.Fatalf("Parse returned error: %v", err)
		}

		for i := range 10 {
			if err := doc.Set(fmt.Sprintf("key%d", i), i); err != nil {
				b.Fatalf("Set returned error: %v", err)
			}
		}
	}
}

// BenchmarkMutate is the typical call pattern for downstream tools: parse,
// mutate, serialize, all in one shot.
func BenchmarkMutate(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	content := []byte("---\ntitle: Example\ncount: 42\n---\nbody\n")
	for b.Loop() {
		_, _, err := Mutate(content, func(doc *Document) error {
			return doc.Set("index", 1)
		})
		if err != nil {
			b.Fatalf("Mutate returned error: %v", err)
		}
	}
}

// BenchmarkSetComplex exercises the Set path with a complex (nested map)
// value, where the avoided cloneNode allocation actually matters.
func BenchmarkSetComplex(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	value := map[string]any{
		"a": 1,
		"b": "two",
		"c": []int{1, 2, 3},
		"d": map[string]any{"nested": true},
	}
	for b.Loop() {
		doc, err := Parse(benchmarkDocument)
		if err != nil {
			b.Fatalf("Parse returned error: %v", err)
		}
		if err := doc.Set("complex", value); err != nil {
			b.Fatalf("Set returned error: %v", err)
		}
		if _, err := doc.Bytes(); err != nil {
			b.Fatalf("Bytes returned error: %v", err)
		}
	}
}
