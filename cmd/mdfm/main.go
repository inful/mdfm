package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/inful/mdfm"
)

var version = "dev"

const exitUsage = 2

type repeatedFlag []string

func (r *repeatedFlag) String() string {
	return strings.Join(*r, ",")
}

func (r *repeatedFlag) Set(value string) error {
	*r = append(*r, value)
	return nil
}

func main() {
	var setPairs repeatedFlag
	var deleteKeys repeatedFlag
	var showVersion bool

	flag.Var(&setPairs, "set", "Set frontmatter key=value (repeatable)")
	flag.Var(&deleteKeys, "delete", "Delete frontmatter key (repeatable)")
	flag.BoolVar(&showVersion, "version", false, "Show version")
	flag.Parse()

	if showVersion {
		_, _ = fmt.Fprintln(os.Stdout, version)
		return
	}

	if flag.NArg() != 1 {
		_, _ = fmt.Fprintf(os.Stderr, "usage: %s [--set key=value] [--delete key] <file>\n", os.Args[0])
		os.Exit(exitUsage)
	}

	path := flag.Arg(0)

	err := mdfm.UpdateFile(path, func(doc *mdfm.Document) error {
		for _, pair := range setPairs {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("invalid --set value %q, expected key=value", pair)
			}
			if err := doc.Set(strings.TrimSpace(key), strings.TrimSpace(value)); err != nil {
				return err
			}
		}

		for _, key := range deleteKeys {
			if _, err := doc.Delete(strings.TrimSpace(key)); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
