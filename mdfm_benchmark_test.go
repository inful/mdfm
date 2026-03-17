package mdfm

import "testing"

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

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := Parse(benchmarkDocument); err != nil {
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
