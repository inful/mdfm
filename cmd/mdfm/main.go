package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/inful/mdfm"
)

var version = "dev"

const (
	exitUsage = 2

	// Names as registered with flag, which must not carry a leading dash.
	setName    = "set"
	deleteName = "delete"

	// Forms as shown in usage and error messages.
	setFlag    = "--" + setName
	deleteFlag = "--" + deleteName
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	var setPairs, deleteKeys []string
	var showVersion bool

	flagSet := flag.NewFlagSet("mdfm", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.Func(setName, "Set frontmatter key=value (repeatable)", func(value string) error {
		setPairs = append(setPairs, value)
		return nil
	})
	flagSet.Func(deleteName, "Delete frontmatter key (repeatable)", func(value string) error {
		deleteKeys = append(deleteKeys, value)
		return nil
	})
	flagSet.BoolVar(&showVersion, "version", false, "Show version")
	if err := flagSet.Parse(args); err != nil {
		return 1
	}

	if showVersion {
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	}

	if flagSet.NArg() != 1 {
		_, _ = fmt.Fprintf(stderr, "usage: %s [%s key=value] [--delete key] <file>\n", flagSet.Name(), setFlag)
		return exitUsage
	}

	path := flagSet.Arg(0)

	err := mdfm.UpdateFile(path, func(doc *mdfm.Document) error {
		for _, pair := range setPairs {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("invalid %s value %q, expected key=value", setFlag, pair)
			}
			// Only the key is trimmed: an intentional leading or trailing
			// space in a value is the user's data, not stray whitespace.
			if err := doc.Set(strings.TrimSpace(key), value); err != nil {
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
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	return 0
}
