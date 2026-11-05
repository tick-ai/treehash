package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	emptyHash   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	exampleHash = "9ea3a9bd92ef97fa0987ea7b8f1ba5791d45246c69ea70f0b64791252ac5e692"
	exampleMani = "x ab08508fdf5ca4da5c4995987bc41c56c048aaa5eeb046417ae4049b7d40286e bin/run.sh\n" +
		"d e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 empty\n" +
		"f 5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03 hello.txt\n" +
		"l 734cad14909bedfafb5b273b6b0eb01fbfa639587d217f78ce9639bba41f4415 latest\n"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "treehash-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "treehash")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build failed:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// th runs the tool in dir and returns stdout, stderr and the exit code.
func th(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	return runCmd(t, exec.Command(binary, args...), dir)
}

func runCmd(t *testing.T, cmd *exec.Cmd, dir string) (string, string, int) {
	t.Helper()
	cmd.Dir = dir
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return so.String(), se.String(), code
}

func write(t *testing.T, root, rel, content string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, root, rel, target string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

func exampleTree(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "hello.txt", "hello\n", 0o644)
	write(t, root, "bin/run.sh", "echo hi\n", 0o755)
	mkdir(t, root, "empty")
	link(t, root, "latest", "hello.txt")
	return root
}

func wantRun(t *testing.T, dir string, wantOut string, wantCode int, args ...string) {
	t.Helper()
	out, errOut, code := th(t, dir, args...)
	if out != wantOut || code != wantCode {
		t.Fatalf("treehash %q: got stdout %q exit %d (stderr %q), want stdout %q exit %d", args, out, code, errOut, wantOut, wantCode)
	}
	if wantCode != 2 && errOut != "" {
		t.Fatalf("treehash %q: unexpected stderr %q", args, errOut)
	}
}

func TestExample(t *testing.T) {
	root := exampleTree(t)
	wantRun(t, root, exampleMani, 0, "manifest", ".")
	wantRun(t, root, exampleHash+"\n", 0, "hash", ".")
	wantRun(t, root, `{"hash":"`+exampleHash+`","entries":4}`+"\n", 0, "hash", ".", "--json")
	want := `{"hash":"` + exampleHash + `","entries":[` +
		`{"type":"x","sha256":"ab08508fdf5ca4da5c4995987bc41c56c048aaa5eeb046417ae4049b7d40286e","path":"bin/run.sh"},` +
		`{"type":"d","sha256":"` + emptyHash + `","path":"empty"},` +
		`{"type":"f","sha256":"5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03","path":"hello.txt"},` +
		`{"type":"l","sha256":"734cad14909bedfafb5b273b6b0eb01fbfa639587d217f78ce9639bba41f4415","path":"latest"}]}` + "\n"
	wantRun(t, root, want, 0, "--json", "manifest", ".")
}

func TestEmptyTree(t *testing.T) {
	root := t.TempDir()
	wantRun(t, root, emptyHash+"\n", 0, "hash", ".")
	wantRun(t, root, "", 0, "manifest", ".")
	wantRun(t, root, `{"hash":"`+emptyHash+`","entries":0}`+"\n", 0, "hash", ".", "--json")
	wantRun(t, root, `{"hash":"`+emptyHash+`","entries":[]}`+"\n", 0, "manifest", ".", "--json")
	// Only ignored things: a pipe.
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantRun(t, root, emptyHash+"\n", 0, "hash", ".")
}

func manifestPaths(t *testing.T, root string, args ...string) []string {
	t.Helper()
	out, _, code := th(t, root, append([]string{"manifest", ".", "--json"}, args...)...)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var doc struct {
		Entries []struct{ Type, Sha256, Path string }
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range doc.Entries {
		paths = append(paths, e.Type+":"+e.Path)
	}
	return paths
}

func TestOrderIsByteWise(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a0", "a.b", "a-b", "a/b", "B", "b", " sp", "é", "z"} {
		write(t, root, n, "x", 0o644)
	}
	got := strings.Join(manifestPaths(t, root), " ")
	want := "f: sp f:B f:a-b f:a.b f:a/b f:a0 f:b f:z f:é"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTypeLetters(t *testing.T) {
	root := t.TempDir()
	write(t, root, "owner", "1", 0o700)
	write(t, root, "ownerx", "1", 0o500)
	write(t, root, "groupx", "1", 0o611)
	write(t, root, "otherx", "1", 0o601)
	write(t, root, "setuid", "1", 0o644|os.ModeSetuid)
	write(t, root, "plain", "1", 0o644)
	write(t, root, "empty.txt", "", 0o644)
	write(t, root, ".hidden", "1", 0o644)
	got := strings.Join(manifestPaths(t, root), " ")
	want := "f:.hidden f:empty.txt f:groupx f:otherx x:owner x:ownerx f:plain f:setuid"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEscaping(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a b/c d.txt", "1", 0o644)
	write(t, root, "back\\slash", "1", 0o644)
	write(t, root, "new\nline", "1", 0o644)
	out, _, code := th(t, root, "manifest", ".")
	if code != 0 {
		t.Fatal(code)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines: %q", lines)
	}
	wantSuffix := []string{" a b/c d.txt", ` back\\slash`, ` new\nline`}
	for i, l := range lines {
		if !strings.HasSuffix(l, wantSuffix[i]) || len(l) != 2+64+len(wantSuffix[i]) {
			t.Errorf("line %d: %q", i, l)
		}
	}
	// JSON paths are not escaped.
	paths := manifestPaths(t, root)
	if paths[1] != "f:back\\slash" || paths[2] != "f:new\nline" {
		t.Errorf("json paths %q", paths)
	}
	// Plain diff paths are escaped, JSON diff paths are not.
	other := t.TempDir()
	wantRun(t, root, "- a b/c d.txt\n- back\\\\slash\n- new\\nline\n", 1, "diff", root, other)
	out, _, _ = th(t, root, "diff", root, other, "--json")
	var d struct{ Removed []string }
	if err := json.Unmarshal([]byte(out), &d); err != nil || d.Removed[1] != "back\\slash" || d.Removed[2] != "new\nline" {
		t.Errorf("diff json %q %v", out, err)
	}
}

func TestJSONNoHTMLEscape(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a<b>&c", "1", 0o644)
	out, _, _ := th(t, root, "manifest", ".", "--json")
	if !strings.Contains(out, `"path":"a<b>&c"`) {
		t.Fatalf("got %q", out)
	}
}

func TestSymlinks(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "real")
	write(t, root, "real/f", "data", 0o644)
	link(t, root, "dirlink", "real")
	link(t, root, "dangling", "nowhere/at all")
	link(t, root, "outside", "/etc")
	got := strings.Join(manifestPaths(t, root), " ")
	if got != "l:dangling l:dirlink l:outside f:real/f" {
		t.Fatalf("got %q", got)
	}
	out, _, _ := th(t, root, "manifest", ".")
	if !strings.Contains(out, "l "+sha256Hex("nowhere/at all")+" dangling\n") {
		t.Fatalf("dangling digest wrong: %q", out)
	}
	if !strings.Contains(out, "l "+sha256Hex("real")+" dirlink\n") {
		t.Fatalf("dirlink digest wrong: %q", out)
	}
}

func TestEmptyFolderRules(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "only-empty/inner")
	mkdir(t, root, "only-excluded/cache")
	mkdir(t, root, "only-pipe")
	if err := syscall.Mkfifo(filepath.Join(root, "only-pipe/p"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(manifestPaths(t, root, "--exclude", "cache"), " ")
	if got != "d:only-empty/inner d:only-excluded d:only-pipe" {
		t.Fatalf("got %q", got)
	}
}

func TestExclude(t *testing.T) {
	root := t.TempDir()
	write(t, root, "keep.txt", "1", 0o644)
	write(t, root, "a/b/cache/x", "1", 0o644)
	write(t, root, "a/cache2/y", "1", 0o644)
	write(t, root, "a/mycache/z", "1", 0o644)
	write(t, root, "a/b/node_modules/m/n", "1", 0o644)
	write(t, root, "a/node_modules", "a file is never skipped", 0o644)
	mkdir(t, root, "real")
	link(t, root, "cache", "real") // a link to a folder is a link
	write(t, root, "cache.txt", "1", 0o644)
	cases := []struct {
		args []string
		want string
	}{
		{nil, "f:a/b/cache/x f:a/b/node_modules/m/n f:a/cache2/y f:a/mycache/z f:a/node_modules l:cache f:cache.txt f:keep.txt d:real"},
		{[]string{"--exclude", "cache"}, "f:a/b/node_modules/m/n f:a/cache2/y f:a/mycache/z f:a/node_modules l:cache f:cache.txt f:keep.txt d:real"},
		{[]string{"--exclude", "cache*"}, "f:a/b/node_modules/m/n f:a/mycache/z f:a/node_modules l:cache f:cache.txt f:keep.txt d:real"},
		{[]string{"--exclude", "*cache*", "--exclude", "node_modules"}, "d:a/b f:a/node_modules l:cache f:cache.txt f:keep.txt d:real"},
		{[]string{"--exclude", "*cache"}, "f:a/b/node_modules/m/n f:a/cache2/y f:a/node_modules l:cache f:cache.txt f:keep.txt d:real"},
		{[]string{"--exclude", "CACHE"}, "f:a/b/cache/x f:a/b/node_modules/m/n f:a/cache2/y f:a/mycache/z f:a/node_modules l:cache f:cache.txt f:keep.txt d:real"},
		{[]string{"--exclude", "a"}, "l:cache f:cache.txt f:keep.txt d:real"},
		{[]string{"--exclude", "**"}, "l:cache f:cache.txt f:keep.txt"},
	}
	for _, c := range cases {
		// the sort order puts a/b/node_modules/m/n before a/cache2/y
		got := strings.Join(manifestPaths(t, root, c.args...), " ")
		if got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.args, got, c.want)
		}
	}
	// The given folder is never skipped, whatever its name.
	sub := filepath.Join(root, "a", "cache2")
	out, _, code := th(t, root, "manifest", sub, "--exclude", "cache2")
	if code != 0 || !strings.Contains(out, " y\n") {
		t.Errorf("given folder skipped: %q", out)
	}
}

func TestChangesChangeTheHash(t *testing.T) {
	base := func() string { return exampleTree(t) }
	hashOf := func(root string) string {
		out, _, code := th(t, root, "hash", ".")
		if code != 0 {
			t.Fatal(code)
		}
		return out
	}
	ref := hashOf(base())
	same := func(name string, mutate func(root string)) {
		root := base()
		mutate(root)
		if got := hashOf(root); (got == ref) != (name[0] == '=') {
			t.Errorf("%s: same=%v", name, got == ref)
		}
	}
	same("=times", func(r string) {
		old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
		os.Chtimes(filepath.Join(r, "hello.txt"), old, old)
		os.Chtimes(filepath.Join(r, "empty"), old, old)
	})
	same("=modes", func(r string) {
		os.Chmod(filepath.Join(r, "hello.txt"), 0o664)
		os.Chmod(filepath.Join(r, "empty"), 0o700)
		os.Chmod(filepath.Join(r, "bin"), 0o700)
		os.Chmod(filepath.Join(r, "bin/run.sh"), 0o755|os.ModeSetuid)
	})
	same("=other-exec", func(r string) { os.Chmod(filepath.Join(r, "hello.txt"), 0o655) })
	same("byte", func(r string) { write(t, r, "hello.txt", "hellO\n", 0o644) })
	same("rename", func(r string) { os.Rename(filepath.Join(r, "hello.txt"), filepath.Join(r, "hello2.txt")) })
	same("move", func(r string) { os.Rename(filepath.Join(r, "hello.txt"), filepath.Join(r, "empty", "hello.txt")) })
	same("exec on", func(r string) { os.Chmod(filepath.Join(r, "hello.txt"), 0o744) })
	same("exec off", func(r string) { os.Chmod(filepath.Join(r, "bin/run.sh"), 0o655) })
	same("target", func(r string) { os.Remove(filepath.Join(r, "latest")); link(t, r, "latest", "bin/run.sh") })
	same("add", func(r string) { write(t, r, "new", "", 0o644) })
	same("add link", func(r string) { link(t, r, "zz", "x") })
	same("add empty dir", func(r string) { mkdir(t, r, "e2") })
	same("remove", func(r string) { os.Remove(filepath.Join(r, "hello.txt")) })
	same("remove empty dir", func(r string) { os.Remove(filepath.Join(r, "empty")) })
}

func TestRootSpellings(t *testing.T) {
	root := exampleTree(t)
	parent := filepath.Dir(root)
	name := filepath.Base(root)
	lnk := filepath.Join(t.TempDir(), "lnk")
	if err := os.Symlink(root, lnk); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ dir, arg string }{
		{root, "."}, {root, "./"}, {root, root}, {root, root + "/"}, {root, root + "//"},
		{parent, name}, {parent, name + "/"}, {parent, "./" + name}, {root, "../" + name},
		{root, lnk}, {root, lnk + "/"},
	} {
		wantRun(t, c.dir, exampleHash+"\n", 0, "hash", c.arg)
	}
}

func TestVerify(t *testing.T) {
	root := exampleTree(t)
	other := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	wantRun(t, root, "ok\n", 0, "verify", ".", exampleHash)
	wantRun(t, root, "mismatch\nexpected: "+other+"\nactual: "+exampleHash+"\n", 1, "verify", ".", other)
	wantRun(t, root, `{"ok":true,"expected":"`+exampleHash+`","actual":"`+exampleHash+`"}`+"\n", 0, "verify", ".", exampleHash, "--json")
	wantRun(t, root, `{"ok":false,"expected":"`+other+`","actual":"`+exampleHash+`"}`+"\n", 1, "verify", ".", other, "--json")

	dir := t.TempDir()
	mf := filepath.Join(dir, "m.txt")
	os.WriteFile(mf, []byte(exampleMani), 0o644)
	wantRun(t, root, "ok\n", 0, "verify", ".", mf)
	mlink := filepath.Join(dir, "m.link")
	os.Symlink(mf, mlink)
	wantRun(t, root, "ok\n", 0, "verify", ".", mlink)
	// Relative manifest file name.
	wantRun(t, dir, "ok\n", 0, "verify", root, "m.txt")
	// One byte changed, or the last newline missing.
	os.WriteFile(mf, []byte(strings.Replace(exampleMani, "hello.txt", "hellp.txt", 1)), 0o644)
	_, _, code := th(t, root, "verify", ".", mf)
	if code != 1 {
		t.Errorf("changed manifest: exit %d", code)
	}
	os.WriteFile(mf, []byte(strings.TrimSuffix(exampleMani, "\n")), 0o644)
	if _, _, code := th(t, root, "verify", ".", mf); code != 1 {
		t.Errorf("manifest without last newline: exit %d", code)
	}
	// A 64-character uppercase text is not a hash: a file name that does not exist.
	upper := strings.ToUpper(exampleHash)
	if _, _, code := th(t, root, "verify", ".", upper); code != 2 {
		t.Errorf("uppercase: exit %d", code)
	}
	// A file named like a hash is still a hash.
	hashNamed := filepath.Join(dir, other)
	os.WriteFile(hashNamed, []byte(exampleMani), 0o644)
	if _, _, code := th(t, dir, "verify", root, other); code != 1 {
		t.Errorf("hash-named file: exit %d", code)
	}
}

func TestDiff(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, a, "same", "1", 0o644)
	write(t, b, "same", "1", 0o644)
	wantRun(t, a, "", 0, "diff", a, b)
	wantRun(t, a, `{"added":[],"removed":[],"changed":[]}`+"\n", 0, "diff", a, b, "--json")

	write(t, a, "gone", "1", 0o644)
	write(t, a, "content", "1", 0o644)
	write(t, b, "content", "2", 0o644)
	write(t, a, "mode", "1", 0o644)
	write(t, b, "mode", "1", 0o755)
	write(t, a, "became-link", "1", 0o644)
	link(t, b, "became-link", "same")
	link(t, a, "target", "one")
	link(t, b, "target", "two")
	write(t, a, "x", "1", 0o644)
	write(t, b, "x/y", "1", 0o644)
	write(t, b, "new", "1", 0o644)
	mkdir(t, b, "newdir")
	// one list sorted by path
	want := "~ became-link\n~ content\n- gone\n~ mode\n+ new\n+ newdir\n~ target\n- x\n+ x/y\n"
	wantRun(t, a, want, 1, "diff", a, b)
	wantRun(t, a, `{"added":["new","newdir","x/y"],"removed":["gone","x"],"changed":["became-link","content","mode","target"]}`+"\n", 1, "diff", a, b, "--json")
}

func TestUsageErrors(t *testing.T) {
	root := exampleTree(t)
	file := filepath.Join(root, "hello.txt")
	cases := [][]string{
		{},
		{"--json"},
		{"frobnicate", "."},
		{"hash"},
		{"hash", ".", "."},
		{"manifest"},
		{"verify", "."},
		{"verify", ".", "a", "b"},
		{"diff", "."},
		{"diff", ".", ".", "."},
		{"hash", "--help"},
		{"--help"},
		{"hash", ".", "-x"},
		{"hash", ".", "--jobs=4"},
		{"hash", ".", "--jobs"},
		{"hash", ".", "--exclude"},
		{"hash", ".", "--jobs", "0"},
		{"hash", ".", "--jobs", "-1"},
		{"hash", ".", "--jobs", "+1"},
		{"hash", ".", "--jobs", "1x"},
		{"hash", ".", "--jobs", " 1"},
		{"hash", ".", "--jobs", ""},
		{"hash", ".", "--jobs", "9223372036854775808"},
		{"hash", ".", "--jobs", "00000000000000000000000000000000"},
		{"hash", ".", "--exclude", ""},
		{"hash", ".", "--exclude", "a/b"},
		{"hash", "no-such-folder"},
		{"hash", file},
		{"hash", ""},
		{"diff", ".", "no-such-folder"},
		{"diff", file, "."},
		{"verify", ".", "no-such-manifest"},
		{"verify", ".", "bin"},
		{"verify", ".", strings.Repeat("a", 63)},
		{"verify", ".", strings.Repeat("a", 65)},
		{"verify", ".", strings.Repeat("g", 64)},
	}
	for _, args := range cases {
		out, errOut, code := th(t, root, args...)
		if code != 2 || out != "" || !strings.HasPrefix(errOut, "treehash: ") || !strings.Contains(errOut, "usage") {
			t.Errorf("%q: exit %d stdout %q stderr %q", args, code, out, errOut)
		}
	}
	// Option values may look like options; options may stand anywhere.
	wantRun(t, root, exampleHash+"\n", 0, "--exclude", "--json", "--jobs", "007", "hash", "--jobs", "9223372036854775807", ".")
}

func TestUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read everything")
	}
	root := t.TempDir()
	write(t, root, "a/ok", "1", 0o644)
	write(t, root, "b/secret", "1", 0o000)
	write(t, root, "c/d/e", "1", 0o644)
	mkdir(t, root, "c/locked")
	os.Chmod(filepath.Join(root, "c/locked"), 0o000)
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "c/locked"), 0o755) })
	for _, args := range [][]string{{"hash", root + "/"}, {"manifest", root}, {"verify", root, exampleHash}, {"diff", root, root}, {"hash", root, "--json"}} {
		out, errOut, code := th(t, root, args...)
		if code != 2 || out != "" {
			t.Fatalf("%q: exit %d stdout %q", args, code, out)
		}
		if !strings.HasPrefix(errOut, "treehash: cannot read "+root+"/b/secret: ") {
			t.Fatalf("%q: stderr %q", args, errOut)
		}
		if !strings.Contains(errOut, "cannot read "+root+"/c/locked: ") {
			t.Fatalf("%q: stderr %q", args, errOut)
		}
		lines := strings.Split(strings.TrimSuffix(errOut, "\n"), "\n")
		if !sort.StringsAreSorted(lines) {
			t.Fatalf("not sorted: %q", lines)
		}
	}
	// Excluding the unreadable folder makes the rest readable.
	os.Chmod(filepath.Join(root, "b/secret"), 0o644)
	wantRun(t, root, "ok\n", 0, "verify", ".", func() string {
		out, _, _ := th(t, root, "hash", ".", "--exclude", "locked")
		return strings.TrimSpace(out)
	}(), "--exclude", "locked")
}

func TestUnreadableRootReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read everything")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mkdir(t, root, "sub")
	os.Chmod(sub, 0o000)
	t.Cleanup(func() { os.Chmod(sub, 0o755) })
	out, errOut, code := th(t, root, "hash", "sub//")
	if code != 2 || out != "" || !strings.HasPrefix(errOut, "treehash: cannot read sub: ") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errOut)
	}
}

func TestUnreadableManifestFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read everything")
	}
	root := exampleTree(t)
	mf := filepath.Join(t.TempDir(), "m")
	os.WriteFile(mf, []byte(exampleMani), 0o000)
	out, errOut, code := th(t, root, "verify", ".", mf)
	if code != 2 || out != "" || !strings.HasPrefix(errOut, "treehash: cannot read "+mf+": ") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errOut)
	}
}

func TestJobsDoNotChangeOutput(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 300; i++ {
		write(t, root, fmt.Sprintf("d%d/f%d", i%7, i), strings.Repeat(fmt.Sprint(i), i), 0o644)
	}
	ref, _, _ := th(t, root, "manifest", ".", "--jobs", "1")
	for _, j := range []string{"2", "3", "16", "0001", "1000", "9223372036854775807"} {
		for k := 0; k < 2; k++ {
			if got, _, _ := th(t, root, "manifest", ".", "--jobs", j); got != ref {
				t.Fatalf("--jobs %s differs", j)
			}
		}
	}
	// Last --jobs wins.
	wantRun(t, root, ref, 0, "manifest", ".", "--jobs", "1", "--jobs", "2")
}

func TestFewOpenFiles(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 160; i++ {
		write(t, root, fmt.Sprintf("d%d/f%03d", i%5, i), fmt.Sprint(i), 0o644)
	}
	ref, _, _ := th(t, root, "hash", ".")
	cmd := exec.Command("sh", "-c", `ulimit -n 64 && exec "$0" "$@"`, binary, "hash", ".", "--jobs", "4")
	out, errOut, code := runCmd(t, cmd, root)
	if code != 0 || out != ref || errOut != "" {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errOut)
	}
}

func TestLargeFile(t *testing.T) {
	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "big"))
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("0123456789abcdef"), 1<<16) // 1 MiB
	h := sha256.New()
	for i := 0; i < 40; i++ {
		f.Write(chunk)
		h.Write(chunk)
	}
	f.Close()
	out, _, code := th(t, root, "manifest", ".")
	want := "f " + hex.EncodeToString(h.Sum(nil)) + " big\n"
	if code != 0 || out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestNothingChanges(t *testing.T) {
	root := exampleTree(t)
	write(t, root, "x/y/z", "1", 0o700)
	link(t, root, "x/l", "nope")
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	for _, p := range []string{"hello.txt", "x/y/z", "x/y", "x", "empty"} {
		os.Chtimes(filepath.Join(root, p), old, old)
	}
	snap := func() string {
		var b strings.Builder
		filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			target, _ := os.Readlink(p)
			content, _ := os.ReadFile(p)
			fmt.Fprintf(&b, "%s %v %d %s %q\n", p, fi.Mode(), fi.ModTime().UnixNano(), target, content)
			return nil
		})
		return b.String()
	}
	before := snap()
	for _, args := range [][]string{{"hash", "."}, {"manifest", "."}, {"verify", ".", exampleHash}, {"diff", ".", "."}} {
		th(t, root, args...)
	}
	if after := snap(); after != before {
		t.Fatalf("tree changed:\n%s\n---\n%s", before, after)
	}
}

func TestMatchName(t *testing.T) {
	for _, c := range []struct {
		p, n string
		ok   bool
	}{
		{"a", "a", true}, {"a", "b", false}, {"*", "", true}, {"*", "abc", true}, {"a*", "a", true},
		{"a*c", "abbc", true}, {"a*c", "abcb", false}, {"*a*b*", "xaxbx", true}, {"*a*b*", "xbxax", false},
		{"a**b", "ab", true}, {"?", "a", false}, {"[a]", "a", false}, {"a.b", "axb", false}, {"A", "a", false},
		{"*é", "café", true}, {"", "", true},
	} {
		if got := matchName(c.p, c.n); got != c.ok {
			t.Errorf("matchName(%q,%q)=%v", c.p, c.n, got)
		}
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
