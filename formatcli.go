package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ensamuel7/dex/format"
)

// runFormat is the `dex format` command. With no arguments it formats every .dx file
// under the working directory, which is the usual thing to want; with arguments
// it formats those files and the .dx files under those directories.
//
// A file is rewritten only when the formatting actually differs, so running it
// over a tree leaves mtimes — and so any watch or build cache — alone for the
// files that were already right.
func runFormat(args []string) {
	paths := args
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var files []string
	for _, path := range paths {
		found, err := collectDexFiles(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		files = append(files, found...)
	}

	changed := 0
	failed := 0
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed++
			continue
		}

		out, err := format.Source(string(src), file)
		if err != nil {
			// The file is left exactly as it was: a file the formatter cannot
			// account for is a file it must not touch.
			fmt.Fprintln(os.Stderr, formatError(err.Error()))
			failed++
			continue
		}
		if out == string(src) {
			continue
		}

		changed++
		if fmtCheck {
			fmt.Println(file)
			continue
		}
		if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed++
		}
	}

	if fmtCheck && changed > 0 {
		fmt.Fprintf(os.Stderr, "%d file(s) not formatted\n", changed)
		os.Exit(1)
	}
	if failed > 0 {
		os.Exit(1)
	}
	if !fmtCheck && changed > 0 {
		fmt.Printf("Formatted %d file(s)\n", changed)
	}
}

// collectDexFiles expands one argument into the .dx files it names. Build output
// and dot-directories are skipped: generated C and a vendored checkout are not
// anybody's source to reformat.
func collectDexFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	var files []string
	err = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := filepath.Base(p)
			if name == "build" || name == "node_modules" || (strings.HasPrefix(name, ".") && name != "." && name != "..") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".dx") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
