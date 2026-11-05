// Command treehash prints one exact hash for a whole folder tree, and lists
// the paths that differ between two trees. It only lists and reads.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const usageLine = "usage: treehash hash|manifest FOLDER | verify FOLDER HASH|MANIFESTFILE | diff FOLDER1 FOLDER2 [--exclude PATTERN]... [--jobs N] [--json]"

type options struct {
	excludes []string
	jobs     int64
	json     bool
}

type usageError struct{ msg string }

func (u *usageError) Error() string { return u.msg }

func usagef(format string, a ...any) error {
	return &usageError{fmt.Sprintf(format, a...)}
}

// parseJobs accepts one or more ASCII digits with a value of 1 to 2^63-1.
func parseJobs(v string) (int64, error) {
	if v == "" {
		return 0, usagef("bad value for --jobs: empty")
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return 0, usagef("bad value for --jobs: %q", v)
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		return 0, usagef("bad value for --jobs: %q", v)
	}
	return n, nil
}

// matchName reports whether name equals pattern, where * in pattern stands
// for any run of bytes, including none.
func matchName(pattern, name string) bool {
	p, n := 0, 0
	star, mark := -1, 0
	for n < len(name) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, n
			p++
		case p < len(pattern) && pattern[p] == name[n]:
			p++
			n++
		case star >= 0:
			mark++
			n = mark
			p = star + 1
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

func (o *options) excluded(name string) bool {
	for _, p := range o.excludes {
		if matchName(p, name) {
			return true
		}
	}
	return false
}

// parseArgs splits the arguments into options, command and operands.
func parseArgs(args []string) (o options, command string, operands []string, err error) {
	o.jobs = int64(runtime.NumCPU())
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--exclude" || a == "--jobs":
			if i+1 >= len(args) {
				return o, "", nil, usagef("option %s needs a value", a)
			}
			i++
			v := args[i]
			if a == "--jobs" {
				if o.jobs, err = parseJobs(v); err != nil {
					return o, "", nil, err
				}
			} else {
				if v == "" || strings.Contains(v, "/") {
					return o, "", nil, usagef("bad pattern for --exclude: %q (not empty, no /)", v)
				}
				o.excludes = append(o.excludes, v)
			}
		case a == "--json":
			o.json = true
		case strings.HasPrefix(a, "-"):
			return o, "", nil, usagef("unknown option %q", a)
		case command == "":
			command = a
		default:
			operands = append(operands, a)
		}
	}
	return o, command, operands, nil
}

type entry struct {
	path string
	typ  byte
	sum  [32]byte
}

type readErr struct{ path, reason string }

func reasonOf(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return strings.ReplaceAll(err.Error(), "\n", " ")
}

type walker struct {
	base    string
	opts    *options
	entries []entry
	errs    []readErr
	mu      sync.Mutex
}

func (w *walker) osPath(rel string) string {
	if rel == "" {
		if w.base == "" {
			return "/"
		}
		return w.base
	}
	return w.base + "/" + rel
}

func (w *walker) shown(rel string) string {
	if rel == "" {
		return w.base
	}
	return w.base + "/" + rel
}

func (w *walker) fail(rel string, err error) {
	w.mu.Lock()
	w.errs = append(w.errs, readErr{w.shown(rel), reasonOf(err)})
	w.mu.Unlock()
}

// walk lists the folder at rel (each folder is closed once listed) and its
// subfolders, and returns the number of entries found under it.
func (w *walker) walk(rel string) int {
	list, err := os.ReadDir(w.osPath(rel))
	if err != nil {
		w.fail(rel, err)
		return 0
	}
	count := 0
	for _, de := range list {
		name := de.Name()
		child := name
		if rel != "" {
			child = rel + "/" + name
		}
		t := de.Type()
		switch {
		case t&fs.ModeSymlink != 0:
			w.entries = append(w.entries, entry{path: child, typ: 'l'})
			count++
		case t.IsDir():
			if w.opts.excluded(name) {
				continue
			}
			n := w.walk(child)
			if n == 0 {
				w.entries = append(w.entries, entry{path: child, typ: 'd', sum: emptySum()})
				n = 1
			}
			count += n
		case t.IsRegular():
			w.entries = append(w.entries, entry{path: child, typ: 'f'})
			count++
		}
	}
	return count
}

func emptySum() [32]byte { return sha256.Sum256(nil) }

// streamSum hashes everything f gives, as a stream.
func streamSum(f io.Reader, buf []byte) ([32]byte, error) {
	h := sha256.New()
	for {
		n, err := f.Read(buf)
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return [32]byte{}, err
		}
	}
	var s [32]byte
	h.Sum(s[:0])
	return s, nil
}

// fileSum opens the regular file at path and returns its type letter and
// the SHA-256 of its content.
func fileSum(path string, buf []byte) (typ byte, sum [32]byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, sum, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, sum, err
	}
	if !fi.Mode().IsRegular() {
		return 0, sum, &fs.PathError{Op: "read", Path: path, Err: errors.New("not a regular file any more")}
	}
	typ = 'f'
	if fi.Mode().Perm()&0o100 != 0 {
		typ = 'x'
	}
	sum, err = streamSum(f, buf)
	return typ, sum, err
}

// hashAll reads the files and links of the sorted entries with at most jobs
// workers, so at most jobs files are open at once.
func (w *walker) hashAll() {
	var todo []int
	for i, e := range w.entries {
		if e.typ == 'f' || e.typ == 'l' {
			todo = append(todo, i)
		}
	}
	n := int64(len(todo))
	if w.opts.jobs < n {
		n = w.opts.jobs
	}
	ch := make(chan int)
	var wg sync.WaitGroup
	for k := int64(0); k < n; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var buf []byte
			for i := range ch {
				e := &w.entries[i]
				p := w.osPath(e.path)
				if e.typ == 'l' {
					target, err := os.Readlink(p)
					if err != nil {
						w.fail(e.path, err)
						continue
					}
					e.sum = sha256.Sum256([]byte(target))
					continue
				}
				if buf == nil {
					buf = make([]byte, 32<<10)
				}
				typ, sum, err := fileSum(p, buf)
				if err != nil {
					w.fail(e.path, err)
					continue
				}
				e.typ, e.sum = typ, sum
			}
		}()
	}
	for _, i := range todo {
		ch <- i
	}
	close(ch)
	wg.Wait()
}

// readTree returns the sorted entries of the tree at folder, with the
// problems found while reading it.
func readTree(folder string, o *options) ([]entry, []readErr) {
	w := &walker{base: strings.TrimRight(folder, "/"), opts: o}
	w.walk("")
	sort.Slice(w.entries, func(i, j int) bool { return w.entries[i].path < w.entries[j].path })
	w.hashAll()
	return w.entries, w.errs
}

var pathEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`)

func manifestBytes(entries []entry) []byte {
	var b bytes.Buffer
	for _, e := range entries {
		b.WriteByte(e.typ)
		b.WriteByte(' ')
		b.WriteString(hex.EncodeToString(e.sum[:]))
		b.WriteByte(' ')
		b.WriteString(pathEscaper.Replace(e.path))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func treeHash(manifest []byte) string {
	s := sha256.Sum256(manifest)
	return hex.EncodeToString(s[:])
}

func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func encodeJSON(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return b.Bytes()
}

type jsonEntry struct {
	Type   string `json:"type"`
	Sha256 string `json:"sha256"`
	Path   string `json:"path"`
}

func jsonEntries(entries []entry) []jsonEntry {
	out := make([]jsonEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, jsonEntry{string(e.typ), hex.EncodeToString(e.sum[:]), e.path})
	}
	return out
}

func checkFolder(arg string) error {
	fi, err := os.Stat(arg)
	if err != nil {
		return usagef("%q: %s", arg, reasonOf(err))
	}
	if !fi.IsDir() {
		return usagef("%q is not a folder", arg)
	}
	return nil
}

// reportErrors writes the read problems, in the order of their paths.
func reportErrors(errs []readErr, stderr io.Writer) int {
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].path < errs[j].path })
	var b bytes.Buffer
	for _, e := range errs {
		fmt.Fprintf(&b, "treehash: cannot read %s: %s\n", e.path, e.reason)
	}
	stderr.Write(b.Bytes())
	return 2
}

func run(args []string, stdout, stderr io.Writer) int {
	code, out, errs, uerr := execute(args)
	if uerr != nil {
		fmt.Fprintf(stderr, "treehash: %s\n%s\n", uerr, usageLine)
		return 2
	}
	if len(errs) > 0 {
		return reportErrors(errs, stderr)
	}
	if _, err := stdout.Write(out); err != nil {
		fmt.Fprintf(stderr, "treehash: cannot write output: %s\n", reasonOf(err))
		return 2
	}
	return code
}

// execute runs a command and returns the exit code and the standard output
// to write, or the read problems, or a usage error.
func execute(args []string) (code int, out []byte, errs []readErr, uerr error) {
	o, command, operands, err := parseArgs(args)
	if err != nil {
		return 0, nil, nil, err
	}
	want := 0
	switch command {
	case "":
		return 0, nil, nil, usagef("no command")
	case "hash", "manifest":
		want = 1
	case "verify", "diff":
		want = 2
	default:
		return 0, nil, nil, usagef("unknown command %q", command)
	}
	if len(operands) != want {
		return 0, nil, nil, usagef("%s takes %d operand(s), got %d", command, want, len(operands))
	}
	if err := checkFolder(operands[0]); err != nil {
		return 0, nil, nil, err
	}
	switch command {
	case "diff":
		if err := checkFolder(operands[1]); err != nil {
			return 0, nil, nil, err
		}
	case "verify":
		if !isHash(operands[1]) {
			fi, err := os.Stat(operands[1])
			if err != nil {
				return 0, nil, nil, usagef("%q is neither a hash nor a manifest file: %s", operands[1], reasonOf(err))
			}
			if !fi.Mode().IsRegular() {
				return 0, nil, nil, usagef("%q is neither a hash nor a manifest file", operands[1])
			}
		}
	}

	entries, errs := readTree(operands[0], &o)
	switch command {
	case "hash":
		if len(errs) > 0 {
			return 0, nil, errs, nil
		}
		h := treeHash(manifestBytes(entries))
		if o.json {
			return 0, encodeJSON(struct {
				Hash    string `json:"hash"`
				Entries int    `json:"entries"`
			}{h, len(entries)}), nil, nil
		}
		return 0, []byte(h + "\n"), nil, nil

	case "manifest":
		if len(errs) > 0 {
			return 0, nil, errs, nil
		}
		m := manifestBytes(entries)
		if o.json {
			return 0, encodeJSON(struct {
				Hash    string      `json:"hash"`
				Entries []jsonEntry `json:"entries"`
			}{treeHash(m), jsonEntries(entries)}), nil, nil
		}
		return 0, m, nil, nil

	case "verify":
		expected := operands[1]
		if !isHash(expected) {
			s, err := fileDigest(expected)
			if err != nil {
				errs = append(errs, readErr{expected, reasonOf(err)})
			} else {
				expected = hex.EncodeToString(s[:])
			}
		}
		if len(errs) > 0 {
			return 0, nil, errs, nil
		}
		actual := treeHash(manifestBytes(entries))
		ok := expected == actual
		code = 1
		if ok {
			code = 0
		}
		if o.json {
			return code, encodeJSON(struct {
				Ok       bool   `json:"ok"`
				Expected string `json:"expected"`
				Actual   string `json:"actual"`
			}{ok, expected, actual}), nil, nil
		}
		if ok {
			return 0, []byte("ok\n"), nil, nil
		}
		return 1, []byte("mismatch\nexpected: " + expected + "\nactual: " + actual + "\n"), nil, nil

	default: // diff
		entries2, errs2 := readTree(operands[1], &o)
		errs = append(errs, errs2...)
		if len(errs) > 0 {
			return 0, nil, errs, nil
		}
		return diffTrees(entries, entries2, o.json)
	}
}

func fileDigest(path string) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	return streamSum(f, make([]byte, 32<<10))
}

// diffTrees compares two sorted trees, old and new, by path.
func diffTrees(oldT, newT []entry, asJSON bool) (int, []byte, []readErr, error) {
	added, removed, changed := []string{}, []string{}, []string{}
	var plain bytes.Buffer
	line := func(mark byte, p string) {
		plain.WriteByte(mark)
		plain.WriteByte(' ')
		plain.WriteString(pathEscaper.Replace(p))
		plain.WriteByte('\n')
	}
	i, j := 0, 0
	for i < len(oldT) || j < len(newT) {
		switch {
		case j >= len(newT) || (i < len(oldT) && oldT[i].path < newT[j].path):
			removed = append(removed, oldT[i].path)
			line('-', oldT[i].path)
			i++
		case i >= len(oldT) || newT[j].path < oldT[i].path:
			added = append(added, newT[j].path)
			line('+', newT[j].path)
			j++
		default:
			if oldT[i].typ != newT[j].typ || oldT[i].sum != newT[j].sum {
				changed = append(changed, newT[j].path)
				line('~', newT[j].path)
			}
			i++
			j++
		}
	}
	code := 0
	if len(added)+len(removed)+len(changed) > 0 {
		code = 1
	}
	if asJSON {
		return code, encodeJSON(struct {
			Added   []string `json:"added"`
			Removed []string `json:"removed"`
			Changed []string `json:"changed"`
		}{added, removed, changed}), nil, nil
	}
	return code, plain.Bytes(), nil, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
