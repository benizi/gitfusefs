# gitfusefs

Expose a Git repo as a directory via FUSE.

## Usage

```sh
go run . path/to/.git path/to/mountpoint
```

### Layout

Root directory is:
- Every commit ID (via `git rev-list --all`)
- Every tree ID pointed to by those commits

Under the ID-named directory:
- Files as named by that commit/tree

E.g.:

```sh
mkdir -p ~/tmp/mounted
go run . some/path/.git ~/tmp/mounted
```

\+ in another terminal
```sh
ls -l ~/tmp/mounted
```
=> (list of object IDs)
```
dr-xr-xr-x 1 root root 2 Jul 27 20:22 000b47b3b163447cb5f6768b8c304e26724f93ea
dr-xr-xr-x 1 root root 2 Jul 27 20:22 000f1a2d96d0d75ad750fe8693c963a42aca0e5d
dr-xr-xr-x 1 root root 2 Jul 27 20:22 0012006e23556fcc565a6ce2d7d265c28d876bff
...
dr-xr-xr-x 1 root root 2 Jul 27 20:22 ffee1fbb6db77c0da06015d355fc0c6baa54cafd
dr-xr-xr-x 1 root root 2 Jul 27 20:22 ffefef3be1b8b43263395f8842516a193abb542c
dr-xr-xr-x 1 root root 2 Jul 27 20:22 fff688949cf0becaefb35984f6a079cb99430513
```

\+ list a tree
```sh
ls -l ~/tmp/gfm/000b47b3b163447cb5f6768b8c304e26724f93ea
```
=> (list of files in [that tree](https://gitlab.freedesktop.org/xkeyboard-config/xkeyboard-config/-/commit/d1a7abd34f79cb0ad27c4861afd98d5b92723be0))
```
total 0
-rw-r--r-- 1 root root    510 Jul 27 20:22 AUTHORS
-rw-r--r-- 1 root root   9244 Jul 27 20:22 COPYING
-rw-r--r-- 1 root root     50 Jul 27 20:22 ChangeLog
-rw-r--r-- 1 root root 116302 Jul 27 20:22 ChangeLog.old
-rw-r--r-- 1 root root    652 Jul 27 20:22 Makefile.am
-rw-r--r-- 1 root root   5070 Jul 27 20:22 NEWS
-rw-r--r-- 1 root root   1627 Jul 27 20:22 README
-rwxr-xr-x 1 root root    365 Jul 27 20:22 autogen.sh
dr-xr-xr-x 1 root root      2 Jul 27 20:22 compat
-rw-r--r-- 1 root root   3575 Jul 27 20:22 configure.ac
dr-xr-xr-x 1 root root      2 Jul 27 20:22 docs
dr-xr-xr-x 1 root root      2 Jul 27 20:22 geometry
dr-xr-xr-x 1 root root      2 Jul 27 20:22 keycodes
dr-xr-xr-x 1 root root      2 Jul 27 20:22 man
-rw-r--r-- 1 root root   2191 Jul 27 20:22 meson.build
-rw-r--r-- 1 root root    256 Jul 27 20:22 meson_options.txt
dr-xr-xr-x 1 root root      2 Jul 27 20:22 po
dr-xr-xr-x 1 root root      2 Jul 27 20:22 rules
dr-xr-xr-x 1 root root      2 Jul 27 20:22 scripts
dr-xr-xr-x 1 root root      2 Jul 27 20:22 symbols
dr-xr-xr-x 1 root root      2 Jul 27 20:22 tests
dr-xr-xr-x 1 root root      2 Jul 27 20:22 types
-rw-r--r-- 1 root root    165 Jul 27 20:22 xkeyboard-config.pc.in
```

</details>

## TODO

- [ ] Use commit date as timestamp
- [ ] Allow non-ID refs at top level (URI-encoded? / under a hierarchy?)
- [ ] Ensure non-UTF-8 filenames are being treated correctly (doubtful)
- [ ] Use a non-ancient FUSE library (didn't realize it was ancient when I picked it)
- [ ] RIIR (prefer Rust over Go these days, but I'm much faster in Go)
- [ ] Cache/trim root directory (haven't tested with any large repos)

## Background

I wrote this on a weekend afternoon, because I wanted to figure out why my XKB configuration had broken years ago.
So, cloned `https://gitlab.freedesktop.org/xkeyboard-config/xkeyboard-config.git` to `~/git/xkeyboard-config`.
- Mounted local clone of that repo at `~/tmp/gfm` (gfm = git FUSE mount):

  ```sh
  go run . ~/git/xkeyboard-config/.git ~/tmp/gfm
  ```

For commits that touched `symbols/pc`:
- Compiled [my keymap] with
  - no system includes `-I`
  - the commit files `-I$HOME/tmp/xkb/$id`
  - and my dotfiles `-I$HOME/dotfiles/xkb`

[my keymap]: https://github.com/benizi/dotfiles/blob/ba8a3e555ded9a5e0d07aa9504e84f0f0bfee629/xkb/symbols/benizi
