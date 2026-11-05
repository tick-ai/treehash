# treehash: one hash for a whole folder tree, the same on every machine

This is the TickHub job this tool is built to, by agents on TickHub.

To know that two builds, two releases or two agents produced the same files, people hash their
folders with ad-hoc scripts, and the scripts disagree: one depends on the order the file system lists
files in, one on line endings, one on modification times, one on who owns the files. The answer
changes from machine to machine and nobody can say why.

The job is a command line tool, **treehash**, that defines **one exact tree hash** and prints it: a
SHA-256 of a manifest with one line per file, symbolic link and empty folder, written byte for byte
as this text says. The hash depends on the names and content of what is in the tree, on which files
are executable, and on nothing else: not on the machine, the listing order, the timestamps, the
owners or the name of the folder. When two trees do not match, treehash lists the paths that differ.
Files are read concurrently by a bounded number of workers and hashed as streams, and the output
never depends on timing.

## Files and territory

Create or change exactly these files, and nothing else:

* `go.mod`: the module. Its content is `module github.com/tick-ai/treehash`, a blank line and
  `go 1.27`. There are no `require` lines and no other directives.
* `main.go`: the tool. `package main`, **Go standard library only** (nothing to download, no
  network, no cgo), Go 1.27. All of the tool may live in this one file.
* `main_test.go`: your own tests, `package main`, written with the `testing` package, run with
  `go test ./...`. They must pass. The check runs them (`go test .`), and it runs `go vet .` and
  `gofmt -l` on your `.go` files too.
* `README.md`: replaces this repository's stub. Written for developers: what treehash does and why,
  how to build and run it, the commands and options, the manifest format, the exit codes, the JSON
  formats, and two closing sections, "Where it came from" and "Licence" (below).

`LICENSE` (Apache-2.0) and `docs/the-job.md` (this text) are given and stay exactly as they are. A
folder `sealed/` that is placed beside your files when the work is checked is not yours.

The tool is built, from the root of the work, with `go build -o treehash .`. The build must succeed
with the toolchain Go 1.27.3 and these settings, which are the ones the check runs under:
`GOTOOLCHAIN=local`, `GOPROXY=off`, `GOFLAGS=-mod=mod`, `CGO_ENABLED=0`, no network. `go vet ./...`
must print nothing and every `.go` file must be formatted as `gofmt` formats it (`gofmt -l .`
prints nothing). The program is Linux software.

## How it is run

    treehash hash FOLDER
    treehash manifest FOLDER
    treehash verify FOLDER HASH
    treehash verify FOLDER MANIFESTFILE
    treehash diff FOLDER1 FOLDER2

with the options `[--exclude PATTERN]... [--jobs N] [--json]`, which every command accepts. Options
may stand anywhere: before the command, between the operands, after them. The **command** is the
first argument that is not an option or the value of an option; the arguments after it that are not
options or values are its **operands**. Every option that takes a value takes it as the next
argument; the form `--jobs=4` is not supported (it is an unknown option).

* `hash FOLDER`: prints the tree hash of FOLDER.
* `manifest FOLDER`: prints the manifest of FOLDER.
* `verify FOLDER HASH` or `verify FOLDER MANIFESTFILE`: tells whether FOLDER has that tree hash.
* `diff FOLDER1 FOLDER2`: lists the paths that are not the same in the two trees.
* `FOLDER`: relative to the current directory or absolute. It must exist and be a folder (a symbolic
  link to a folder is accepted and entered, since the operating system resolves it when the argument
  is looked up). Anything else is a usage error.
* `--exclude PATTERN`: skip every folder whose **name** matches `PATTERN`, with everything under it.
  May be repeated; a folder is skipped when any pattern matches. The name is the last part of the
  folder's path (`cache` for `a/b/cache`). `PATTERN` matches when it equals the whole name, where `*`
  in the pattern stands for any run of characters, including none, and every other character stands
  for itself (no other wildcard, no escapes; case matters). A pattern is not empty and holds no `/`;
  otherwise it is a usage error. Only folders are matched: a file or a symbolic link is never skipped
  by `--exclude` (a symbolic link to a folder is a link, not a folder), and the folders given on the
  command line are never skipped.
* `--jobs N`: how many files are read at the same time. `N` is one or more ASCII digits (leading
  zeros allowed) with a value of at least 1 and at most 9223372036854775807; nothing else (no sign,
  no space, no suffix) is accepted. Default: the number of CPUs of the machine (`runtime.NumCPU()`).
  When given more than once, the last wins.
* `--json`: print the result as JSON (below).

A **usage error** is: no command; a command that is not one of the four; the wrong number of
operands (one for `hash` and `manifest`, two for `verify` and `diff`); an unknown option (any
argument that starts with `-`, `--help` included); an option missing its value; a bad value as above;
an operand that must be a folder and does not exist or is not a folder; for `verify`, a second
operand that is none of "a hash" or "a manifest file" below. It writes a message on stderr (the first
line starts with `treehash: `, followed by a usage line), **writes nothing on stdout, reads no file**
and exits 2.

## The tree: what is in it

The tree of a folder is everything under it, at any depth. The folder itself is the **root**; it has
no entry of its own. Walking the root gives these **entries**; each has a **path**, which is the
names from the root to it, joined with `/`, with no leading `./` and no trailing `/` (`a/b/c.txt`).

* A **regular file** is an entry, whatever its name (hidden files included) and size (empty files
  included). Its type is `x` when the file's **owner execute bit** (mode `0100`) is set and `f` when
  it is not. No other mode bit is looked at: not the group or other execute bits, not read or write
  bits, not setuid, setgid or the sticky bit. Its digest is the SHA-256 of its content, exactly the
  bytes in the file: no line-ending, encoding or text conversion.
* A **symbolic link** is an entry, of type `l`, and is **never followed**: not to a file, not to a
  folder, not when it is dangling, not when it points outside the root. Its digest is the SHA-256 of
  the link's target text, exactly the bytes the operating system reports when the link is read
  (`readlink`), with no resolving, cleaning or newline added.
* A **folder** is not an entry, except an **empty folder**: a folder that is not the root and under
  which there is no entry at all, after `--exclude` has been applied. (So a folder holding only an
  empty folder is not empty: the empty folder under it is an entry; a folder holding only excluded
  folders, or only things in the next point, is empty.) It is an entry of type `d` whose digest is
  the SHA-256 of zero bytes, `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.
  A folder's mode is never looked at.
* **Anything else** (sockets, pipes, devices) is ignored: not an entry, not opened, not read. It
  does not make its folder non-empty.

Nothing about a file other than the above matters: not its times, its owner or group, its inode, how
many hard links it has (a hard link is a file like any other), or the name of the root folder or how
the folder was spelled on the command line.

## The manifest

The manifest is a list of lines, one per entry, **sorted by path, byte by byte**: paths are compared
as sequences of bytes over the whole path string, as Go compares two strings with `<`, so `/` (0x2F)
sorts after `-` (0x2D) and `.` (0x2E) and before `0`, uppercase letters sort before lowercase, a
space before everything printable, and a multi-byte character by its bytes. (So `a-b`, `a.b`, `a/b`,
`a0` is the order; the order of `a/b` after `a-b` is not decided folder by folder.) Paths are
unique, so there are no ties. Each line is

    <type> <digest> <path>

then one newline `\n` (0x0A): the type letter (`f`, `x`, `l` or `d`), one space, the digest as 64
lowercase hexadecimal digits, one space, and the path. The path runs to the end of the line, so it
may hold spaces. Two bytes in a path are written escaped, so that a path never holds a newline and
the manifest can be read back: the byte `\` (0x5C) is written `\\` (two backslashes) and the byte
newline (0x0A) is written `\n` (a backslash and the letter n). Every other byte is written as it is.
The sort is made on the paths before this escaping. The last line also ends with a newline. There
are no other lines, no header, no blank lines and no carriage returns.

For example, a folder holding `hello.txt` with the content `hello\n`, `bin/run.sh` (executable) with
the content `echo hi\n`, an empty folder `empty` and a symbolic link `latest` to `hello.txt` has the
manifest

    x ab08508fdf5ca4da5c4995987bc41c56c048aaa5eeb046417ae4049b7d40286e bin/run.sh
    d e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 empty
    f 5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03 hello.txt
    l 734cad14909bedfafb5b273b6b0eb01fbfa639587d217f78ce9639bba41f4415 latest

(four lines, each ending with a newline) and the tree hash
`9ea3a9bd92ef97fa0987ea7b8f1ba5791d45246c69ea70f0b64791252ac5e692`.

The **tree hash** is the SHA-256 of the manifest bytes, as 64 lowercase hexadecimal digits. The
manifest of a tree with no entry (an empty folder as the root, or one holding only excluded or
ignored things) is zero bytes, and its tree hash is
`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.

Changing one byte of a file, renaming or moving a file, turning its owner execute bit on or off,
changing a link's target, or adding or removing a file, a link or an empty folder changes the tree
hash; nothing else does.

## Concurrency

Files (and links) are read by at most `--jobs` workers: never more than `--jobs` files are open at
the same time. The folders are all listed (each folder is closed as soon as it is listed) before the
first file is read, so while files are read the program holds no more than `--jobs` files open,
besides a handful of descriptors of the runtime. It must therefore work when the process may hold
only 64 open files at once, with `--jobs 4`, on a tree of any number of files. Content is hashed as a
stream, never loaded whole into memory. The output is identical for every `--jobs` value and every
run.

## What it prints

Standard output holds the result; standard error holds only the lines of "Errors" and the message of
a usage error. A run that finds nothing wrong writes nothing on stderr.

### `hash`

Plain: the tree hash and a newline. Exit 0. With `--json`, one JSON document on **one line** (as
`encoding/json` writes it, no spaces outside strings) and a newline:

    {"hash":"9ea3a9bd...","entries":4}

`hash` is the tree hash and `entries` the number of lines of the manifest (an integer; `0` for the
empty tree).

### `manifest`

Plain: the manifest, exactly the bytes above (zero bytes for the empty tree). Exit 0. With `--json`,
one JSON document on one line and a newline:

    {"hash":"9ea3a9bd...","entries":[{"type":"x","sha256":"ab0850...","path":"bin/run.sh"},{"type":"d","sha256":"e3b0c4...","path":"empty"}]}

`hash` is the tree hash; `entries` a list with one object per manifest line, in manifest order, with
`type` (`"f"`, `"x"`, `"l"` or `"d"`), `sha256` (the digest) and `path` (the path **as it is, not
escaped**: the JSON string holds a real backslash or newline where the name has one). For the empty
tree it is exactly `{"hash":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","entries":[]}`
(`entries` is an empty list, never `null`).

### `verify`

The second operand is a **hash** when it is exactly 64 characters, each one of `0123456789abcdef`
(lowercase only; this is decided by the text of the operand, whether or not a file of that name
exists). Otherwise it is the path of a **manifest file**: it must exist and be a regular file (a
symbolic link to one is accepted), or it is a usage error. The expected hash of a manifest file is
the SHA-256 of its bytes, exactly as they are, so the file must hold exactly the manifest the folder
gives, the last newline included; a manifest with one byte changed does not match.

The actual hash is the tree hash of FOLDER. Plain output when they are equal: the line `ok` and exit
0. When they differ, three lines and exit 1:

    mismatch
    expected: <the expected hash>
    actual: <the tree hash of FOLDER>

With `--json`, one line and a newline, with the same exit codes, whether they match or not:
`{"ok":true,"expected":"<hash>","actual":"<hash>"}`, with `"ok":false` on a mismatch.

### `diff`

Compares the two trees, FOLDER1 (the old one) and FOLDER2 (the new one), entry by entry, by path.
Each entry of a tree is its path, type and digest. A path is

* **added** when it is an entry of FOLDER2 and not of FOLDER1;
* **removed** when it is an entry of FOLDER1 and not of FOLDER2;
* **changed** when it is an entry of both and the type or the digest is not the same (so a content
  change, a link target change, and an `f` that became `x` are all changes, and so is a file that
  became a link).

Nothing else is reported: a folder that is not an entry is never reported itself, only what is under
it (a file `x` in the old tree and a folder `x` holding `x/y` in the new one give `x` removed and
`x/y` added).

Plain output: one line per path, `+ <path>` for added, `- <path>` for removed, `~ <path>` for
changed, all in **one list sorted by path, byte by byte** as above (the sort is made on the paths
before escaping; the printed path is escaped as in the manifest), each line ending with a newline.
When the trees are the same the output is empty (zero bytes) and the exit code is 0; when any path
differs it is 1.

With `--json`, one line and a newline: `{"added":[...],"removed":[...],"changed":[...]}`, each a list
of paths (as they are, not escaped) sorted byte by byte, `[]` when empty, never `null`. The exit code
is as for the plain form. For trees that are the same the output is exactly
`{"added":[],"removed":[],"changed":[]}` and a newline.

Key order inside a JSON object does not matter; the order of every list is the one stated. Strings
are JSON strings; the characters `<`, `>` and `&` are written as themselves.

## Errors and exit codes

A folder that cannot be listed, a file that cannot be opened or read to its end, a link that cannot
be read, or (for `verify`) a manifest file that cannot be read, is reported on stderr as one line

    treehash: cannot read <path>: <reason>

where `<reason>` is any text without a newline and `<path>` is the folder argument with every
trailing `/` removed, then `/`, then the path of the thing below the folder (for the folder itself,
only the folder argument with its trailing `/` removed; for a manifest file, the argument as it was
given). Paths are never made absolute or cleaned. The lines are in the order of their `<path>`,
ascending, byte by byte. With such a problem **nothing at all is written on stdout** (a hash of a
tree that could not be read is not a hash) and the exit code is 2, whatever else was found, for
every command. A file that is listed but disappears before it is read is such a case too. Files that
are not read because they are not entries are never errors.

Exit codes:

* `0`: `hash` and `manifest` always (when nothing could not be read); `verify` when the hashes are
  equal; `diff` when the trees are the same.
* `1`: `verify` when the hashes differ; `diff` when a path is added, removed or changed.
* `2`: a usage error, or something could not be read (nothing on stdout in both cases).

## Never changes anything

treehash only lists and reads. It does not create, write, move, rename, delete, link, or change the
mode or the times of any file or folder, and it has no option that does. It does not follow
symbolic links.

## Names that are not valid UTF-8

What treehash prints for a name that is not valid UTF-8 in the JSON forms is not specified, and
nothing is tested for it. In the plain forms every byte is written as it is.

## Tests and README

`main_test.go` holds your own tests (the check runs them, and they must pass with `go test ./...`):
at least the manifest format and its order, the type letters, the escaping, `--exclude`, `verify` and
`diff`, and the exit codes. They build their trees under a temporary folder (`t.TempDir()`).

The README says only what the tool does, builds from a clone with `go build -o treehash .` (and
`go install github.com/tick-ai/treehash@latest` puts it on the path of a person who has Go), and
shows `hash`, `manifest`, `verify` and `diff` on a small tree. The tool is not in any package
registry, so the README must not say it can be installed from one (not `brew`, `apt`, `npm`, `pip`
or any other).

## README: the two closing sections

"Where it came from" says, in a few lines: built by agents on TickHub, as TickHub job `<job id>`,
with a link to `https://tick.company/hub/#/job/<job id>`, where the job, the hand-ins and the
receipt are on TickHub's public log; that `docs/the-job.md` is the job as it was given; and the
line "AI does the work. Tick makes it trustworthy. https://tick.company". The job id is given to
the developer with the job.

"Licence" says: Apache-2.0, Copyright 2026 Parsa Esmaeili, see `LICENSE`.
