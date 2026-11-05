# treehash

**treehash prints one exact hash for a whole folder tree, the same on every machine, and lists the files that differ when two trees do not match.**

    treehash hash FOLDER

```
$ treehash manifest demo
x ab08508fdf5ca4da5c4995987bc41c56c048aaa5eeb046417ae4049b7d40286e bin/run.sh
d e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 empty
f 5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03 hello.txt
l 734cad14909bedfafb5b273b6b0eb01fbfa639587d217f78ce9639bba41f4415 latest
$ treehash hash demo
9ea3a9bd92ef97fa0987ea7b8f1ba5791d45246c69ea70f0b64791252ac5e692
```

## Why

To know that two builds, two releases or two agents produced the same files, people hash their
folders with ad-hoc scripts, and the scripts disagree: one depends on the order the file system
lists files in, one on line endings, one on modification times, one on who owns the files.

treehash defines one tree hash: the SHA-256 of a manifest with one line per file, symbolic link and
empty folder, written byte for byte as described below. The hash depends on the names and content of
what is in the tree, on which files are executable, and on nothing else: not on the machine, the
listing order, the timestamps, the owners or the name of the folder.

Files are read concurrently by a bounded number of workers and hashed as streams (never loaded whole
into memory), and the output never depends on timing. treehash only lists and reads: it never
creates, writes, moves, deletes or changes anything, and it never follows a symbolic link.

## Build and run

You need Go 1.27 or later. treehash uses only the Go standard library and is Linux software. From a
clone:

    go build -o treehash .
    ./treehash hash FOLDER

With Go installed you can also put it on your path with

    go install github.com/tick-ai/treehash@latest

treehash is not in any package registry.

## Commands

    treehash hash FOLDER
    treehash manifest FOLDER
    treehash verify FOLDER HASH
    treehash verify FOLDER MANIFESTFILE
    treehash diff FOLDER1 FOLDER2

Every command accepts the options `[--exclude PATTERN]... [--jobs N] [--json]`. Options may stand
anywhere: before the command, between the operands, after them. An option that takes a value takes
it as the next argument; `--jobs=4` is not supported (it is an unknown option).

- `hash FOLDER` prints the tree hash of FOLDER.
- `manifest FOLDER` prints the manifest of FOLDER.
- `verify FOLDER HASH` or `verify FOLDER MANIFESTFILE` tells whether FOLDER has that tree hash. The
  second operand is a hash when it is exactly 64 characters of `0123456789abcdef` (decided by the text
  alone); otherwise it is the path of a manifest file, which must exist and be a regular file (or a
  link to one). The expected hash of a manifest file is the SHA-256 of its bytes as they are, so it must
  hold exactly the manifest the folder gives, the last newline included.
- `diff FOLDER1 FOLDER2` lists the paths that are not the same in the two trees.

`FOLDER` is relative to the current directory or absolute, and must exist and be a folder (a symbolic
link to a folder is accepted and entered). However the folder is spelled, the hash is the same.

### Options

- `--exclude PATTERN` skips every folder whose **name** (the last part of its path, `cache` for
  `a/b/cache`) matches PATTERN, with everything under it. `*` stands for any run of characters,
  including none; every other character stands for itself (no other wildcard, no escapes; case
  matters). May be repeated; a folder is skipped when any pattern matches. A pattern is not empty and
  holds no `/`. Only folders are matched: a file or a symbolic link is never skipped, and the folders
  given on the command line are never skipped.
- `--jobs N` is how many files are read at the same time: one or more ASCII digits, from 1 to
  9223372036854775807. Default: the number of CPUs. When given more than once, the last wins.
- `--json` prints the result as JSON (see below).

## The tree

The folder given is the **root**; it has no entry of its own. Everything under it, at any depth, gives
these entries, each with a path (the names from the root joined with `/`, no leading `./`, no trailing
`/`):

| type | what | digest |
| --- | --- | --- |
| `f` | a regular file whose owner execute bit (mode `0100`) is not set | SHA-256 of its content |
| `x` | a regular file whose owner execute bit is set | SHA-256 of its content |
| `l` | a symbolic link, never followed (dangling or not) | SHA-256 of the link's target text |
| `d` | an empty folder (not the root; nothing under it after `--exclude`) | SHA-256 of zero bytes |

No other mode bit is looked at, and a folder's mode never is. Sockets, pipes and devices are ignored:
not an entry, not opened. Times, owners, inodes, hard links and the name of the root never matter.
Changing a byte of a file, renaming or moving it, turning its owner execute bit on or off, changing a
link's target, or adding or removing a file, a link or an empty folder changes the tree hash; nothing
else does.

## The manifest

One line per entry, sorted by path **byte by byte** over the whole path (as Go compares strings with
`<`, so `a-b`, `a.b`, `a/b`, `a0` is the order):

    <type> <digest> <path>\n

The type letter, one space, the digest as 64 lowercase hexadecimal digits, one space, and the path to
the end of the line (it may hold spaces). The byte `\` is written `\\` and a newline in a name is
written `\n`; every other byte is written as it is. The sort is made before this escaping. The last
line also ends with a newline; there is no header, no blank line and no carriage return.

The **tree hash** is the SHA-256 of the manifest bytes, as 64 lowercase hexadecimal digits. A tree
with no entry has an empty manifest and the hash
`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.

## Example

`demo` holds `hello.txt` (the text `hello` and a newline), `bin/run.sh` (executable, `echo hi`), an
empty folder `empty` and a symbolic link `latest` to `hello.txt`:

```
$ treehash hash demo
9ea3a9bd92ef97fa0987ea7b8f1ba5791d45246c69ea70f0b64791252ac5e692
$ treehash verify demo 9ea3a9bd92ef97fa0987ea7b8f1ba5791d45246c69ea70f0b64791252ac5e692
ok
$ treehash manifest demo > demo.manifest
$ treehash verify demo demo.manifest
ok
$ cp -a demo demo2
$ echo changed >> demo2/hello.txt
$ touch demo2/new.txt
$ treehash diff demo demo2
~ hello.txt
+ new.txt
$ echo $?
1
$ treehash verify demo2 demo.manifest
mismatch
expected: 9ea3a9bd92ef97fa0987ea7b8f1ba5791d45246c69ea70f0b64791252ac5e692
actual: 748bca2eb7a864db8fc0dbf26f178012b48cdeed78b5abeb9da4b4b67cba6fcf
$ echo $?
1
```

## Output

Standard output holds the result; standard error holds only error lines. A run that finds nothing
wrong writes nothing on stderr.

- `hash`: the tree hash and a newline.
- `manifest`: the manifest (zero bytes for the empty tree).
- `verify`: `ok` when equal, otherwise three lines `mismatch`, `expected: <hash>`, `actual: <hash>`.
- `diff`: one line per path, `+ <path>` added (in FOLDER2 only), `- <path>` removed (in FOLDER1
  only), `~ <path>` changed (in both, with another type or digest), in one list sorted by path, the
  printed path escaped as in the manifest. Empty output when the trees are the same. A folder that is
  not an entry is never reported, only what is under it.

### JSON (`--json`)

Each result is one JSON document on one line, then a newline. Paths in JSON are not escaped. Key
order inside an object does not matter; `<`, `>` and `&` are written as themselves.

    hash:      {"hash":"9ea3a9bd...","entries":4}
    manifest:  {"hash":"9ea3a9bd...","entries":[{"type":"x","sha256":"ab0850...","path":"bin/run.sh"},...]}
    verify:    {"ok":true,"expected":"<hash>","actual":"<hash>"}
    diff:      {"added":[...],"removed":[...],"changed":[...]}

`entries` in `hash` is the number of manifest lines; in `manifest` it is a list in manifest order
(`[]` for the empty tree, never `null`). The `diff` lists are sorted byte by byte and are `[]` when
empty: for the same trees the output is exactly `{"added":[],"removed":[],"changed":[]}`. `verify`
writes `"ok":false` on a mismatch.

## Exit codes

| code | meaning |
| --- | --- |
| 0 | `hash` and `manifest` always; `verify` when the hashes are equal; `diff` when the trees are the same |
| 1 | `verify` when the hashes differ; `diff` when a path is added, removed or changed |
| 2 | a usage error, or something could not be read; nothing is written on stdout |

A **usage error** is: no command; a command that is not one of the four; the wrong number of
operands; an unknown option (any argument starting with `-`, `--help` included); an option missing its
value; a bad value or pattern; a FOLDER that does not exist or is not a folder; for `verify`, a second
operand that is neither a hash nor a manifest file. It writes a message on stderr whose first line
starts with `treehash: ` and is followed by a usage line, and reads no file.

A folder that cannot be listed, a file that cannot be opened or read to its end, a link that cannot
be read, or a manifest file that cannot be read is reported on stderr, one line each, in the order of
their paths:

    treehash: cannot read <path>: <reason>

`<path>` is the folder argument without its trailing `/`, then `/`, then the path below it (for a
manifest file, the argument as given). Nothing at all is written on stdout and the exit code is 2: a
hash of a tree that could not be read is not a hash.

## Concurrency

Files and links are read by at most `--jobs` workers, and never more than `--jobs` files are open at
once. All folders are listed (each closed as soon as it is listed) before the first file is read, so it
works on a tree of any size when the process may hold only 64 open files, with `--jobs 4`. The output
is identical for every `--jobs` value and every run.

Names that are not valid UTF-8 are written byte for byte in the plain forms; the JSON forms are not
specified for them.

## Where it came from

Built by agents on TickHub, as TickHub job `189e7145-79de-4106-8f32-41e2b79a4f04`. The job, the
hand-ins and the receipt are on TickHub's public log:
https://tick.company/hub/#/job/189e7145-79de-4106-8f32-41e2b79a4f04

`docs/the-job.md` is the job as it was given.

AI does the work. Tick makes it trustworthy. https://tick.company

## Licence

Apache-2.0. Copyright 2026 Parsa Esmaeili. See `LICENSE`.
