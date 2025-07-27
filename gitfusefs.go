package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	iofs "io/fs"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

type debugging int

var dbg debugging

func (d debugging) Printf(f string, args ...interface{}) {
	d.PrintIf(1, f, args...)
}

func (d debugging) PrintIf(lev int, f string, args ...interface{}) {
	if int(d) >= lev {
		log.Printf(f, args...)
	}
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

var gfs gitfs

func main() {
	var revtest bool
	flag.BoolVar(&revtest, "revtest", revtest, "Just test rev-list parsing")
	flag.Parse()

	if flag.NArg() != 2 {
		log.Fatal("Usage: prog gitdir mountpoint")
	}

	gitdir := flag.Arg(0)
	mountpoint := flag.Arg(1)
	gfs = gitfs{gitdir}

	if revtest {
		testrevlist(gfs)
	}

	handle, err := fuse.Mount(
		mountpoint,
		fuse.FSName("gitfs"),
		fuse.Subtype("gitfs"),
	)
	check(err)
	defer handle.Close()
	check(fs.Serve(handle, gfs))
}

func testrevlist(gfs gitfs) {
	objs, err := gfs.revlistall()
	check(err)
	for i, o := range objs {
		fmt.Printf("[%05d]=%v\n", i, o)
	}
	os.Exit(0)
}

type gitkind string

const (
	gitcommit gitkind = "commit"
	gittree           = "tree"
	gitblob           = "blob"
)

type gitobj struct {
	kind gitkind
	id   string
	tree string
	mode int64
	data []byte
}

func parserevlist(data []byte) ([]gitobj, error) {
	objs := []gitobj{}
	for len(data) > 0 {
		cline, rest, found := bytes.Cut(data, []byte("\n"))
		if !found || len(cline) == 0 {
			break
		}
		mkerr := func(msg string) error {
			return fmt.Errorf("Invalid rev-list line: %v (err: %s)", cline, msg)
		}
		parts := strings.Split(string(cline), " ")
		perr := ""
		switch {
		case len(parts) != 3:
			perr = fmt.Sprintf("number of parts (%d)", len(parts))
		case len(parts[0]) != 40: // FIXME(sha)
			perr = fmt.Sprintf("invalid ID format (len=%d)", len(parts[0]))
		case parts[1] != "commit":
			perr = fmt.Sprintf("invalid object type (%s)", parts[1])
		}
		if perr != "" {
			return nil, mkerr(perr)
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil {
			perr = fmt.Sprintf("invalid size (%s)", parts[2])
		}
		if len(rest) < size {
			perr = fmt.Sprintf("invalid size (%s; |rest|=%d)", parts[2], len(rest))
		}

		tree, err := parsecommittree(rest[:size])
		if err != nil {
			perr = fmt.Sprintf("couldn't find tree (%v)", err)
		}
		if perr != "" {
			return nil, mkerr(perr)
		}
		objs = append(objs, gitobj{kind: gitcommit, id: string(parts[0]), tree: tree})
		data = rest[size+1:]
	}
	return objs, nil
}

func parsecommittree(data []byte) (string, error) {
	header, _, found := bytes.Cut(data, []byte("\n\n"))
	if !found {
		return "", fmt.Errorf("no header separator in commit?")
	}
	for _, l := range strings.Split(string(header), "\n") {
		parts := strings.Split(l, " ")
		if len(parts) == 2 && parts[0] == "tree" {
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("no tree in commit?")
}

type gitfs struct {
	gitdir string
}

type dir struct {
	tree string
	path string
}

type file struct {
	name string // FIXME(utf-8): assumes UTF-8?
	id   string
	size uint64
	mode int64
	data []byte
}

func (gitfs) Root() (fs.Node, error) {
	return dir{}, nil
}

func (g gitfs) git(args ...string) ([]byte, error) {
	return g.gitstdin(nil, args...)
}

func (g gitfs) gitstdin(stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_DIR="+g.gitdir)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (g gitfs) revlistall() ([]gitobj, error) {
	// FIXME(nul): use NUL-delimited
	revlist, err := g.git("rev-list", "--all")
	if err != nil {
		return nil, err
	}
	info, err := g.gitstdin(revlist, "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	return parserevlist(info)
}

func (g gitfs) rootlisting() ([]fuse.Dirent, error) {
	objs, err := g.revlistall()
	if err != nil {
		return nil, err
	}
	ents := []fuse.Dirent{}
	for _, obj := range objs {
		cent := fuse.Dirent{
			// FIXME(inodes): Inode: 1 + uint64(len(ents)),
			Name: obj.id,
			Type: fuse.DT_Link,
		}
		tent := fuse.Dirent{
			// FIXME(inodes): Inode: cent.Inode + 1,
			Name: obj.tree,
			Type: fuse.DT_Dir,
		}
		ents = append(ents, cent, tent)
	}
	return ents, nil
}

func (g gitfs) treeobjs(tree string) ([]gitobj, error) {
	data, err := g.git("ls-tree", "-z", tree)
	if err != nil {
		return nil, err
	}
	return parsetreeobjs(data)
}

func parsetreeobjs(data []byte) ([]gitobj, error) {
	objs := []gitobj{}
	for len(data) > 0 {
		info, rest, found := bytes.Cut(data, []byte("\t"))
		if !found {
			return nil, fmt.Errorf("No tab in infoline")
		}
		data = rest

		parts := strings.Split(string(info), " ")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid length (%d)", len(parts))
		}
		mode, err := strconv.ParseInt(parts[0], 8, 64)
		if err != nil {
			return nil, err
		}
		kind := gitkind(parts[1])
		if kind != gitblob && kind != gittree {
			return nil, fmt.Errorf("invalid type (%v)", kind)
		}
		// FIXME(sha): assumes SHA1
		sha := parts[2]
		if len(sha) != 40 {
			return nil, fmt.Errorf("invalid SHA (%v)", sha)
		}

		name, rest, found := bytes.Cut(data, []byte("\x00"))
		if !found {
			return nil, fmt.Errorf("No NUL delimiting name")
		}
		data = rest

		obj := gitobj{kind: kind, id: sha, mode: mode, data: name}
		objs = append(objs, obj)
	}
	return objs, nil
}

func (g gitfs) treelisting(tree, path string) ([]fuse.Dirent, error) {
	actualtree := tree
	parts := strings.Split(path, "/")
	dbg.Printf("treelisting(%v, %v) (parts=%v)", tree, path, parts)

part:
	for len(parts) > 0 {
		if parts[0] == "" {
			parts = parts[1:]
			continue part
		}

		dbg.PrintIf(2, "treelisting looking for part (%v)", parts[0])
		objs, err := g.treeobjs(actualtree)
		if err != nil {
			return nil, err
		}
		for _, o := range objs {
			if o.kind != gittree {
				continue
			}
			if string(o.data) == parts[0] {
				actualtree = o.id
				parts = parts[1:]
				continue part
			}
		}
		return nil, fmt.Errorf("Missing subtree (%s) in %v", parts[0], path)
	}

	objs, err := gfs.treeobjs(actualtree)
	if err != nil {
		return nil, fmt.Errorf("Missing tree (%s)", actualtree)
	}
	ents := []fuse.Dirent{}
	for _, o := range objs {
		kind := fuse.DT_File
		switch o.mode {
		case 0o100644, 0o100755:
			// file
		case 0o120000:
			kind = fuse.DT_Link
		case 0o040000:
			kind = fuse.DT_Dir
		default:
			log.Printf("Unhandled mode (%d = 0o%07o)", o.mode, o.mode)
		}
		ent := fuse.Dirent{
			// FIXME(inodes): Inode: 1 + uint64(len(ents)),
			// FIXME(utf-8): issues with non-UTF-8?
			Name: string(o.data),
			Type: kind,
		}
		ents = append(ents, ent)
	}
	return ents, nil
}

func (g gitfs) nodefor(sha, path, name string) (fs.Node, error) {
	dbg.PrintIf(2, "nodefor(sha=%v, path=%v, name=%v)", sha, path, name)
	fullpath := name
	if path != "" {
		fullpath = path + "/" + name
	}
	data, err := g.git("ls-tree", "-z", sha, fullpath)
	if err != nil {
		log.Printf("ls-tree failed for (sha=%s) (fullpath=%v)", sha, fullpath)
		return nil, syscall.ENOENT
	}
	objs, err := parsetreeobjs(data)
	if err != nil {
		log.Printf("parsetreeobjs failed for (sha=%s) (fullpath=%v)", sha, fullpath)
		return nil, syscall.ENOENT
	}
	if len(objs) != 1 {
		log.Printf("parsetreeobjs -> |%d|!=1", len(objs))
		return nil, syscall.ENOENT
	}
	o := objs[0]
	sdata, err := g.git("cat-file", "-s", o.id)
	if err != nil {
		log.Printf("error getting size(%v): %v", o.id, err)
		return nil, syscall.ENOENT
	}
	if len(sdata) > 0 && sdata[len(sdata)-1] == '\n' {
		sdata = sdata[:len(sdata)-1]
	}
	size, err := strconv.ParseInt(string(sdata), 10, 64)
	if err != nil {
		log.Printf("error getting size(%v): %v", sdata, err)
		return nil, syscall.ENOENT
	}
	mode := o.mode
	if mode == 0o040000 {
		return &dir{tree: sha, path: fullpath}, nil
	}
	return &file{
		name: name,
		id:   o.id,
		mode: mode,
		size: uint64(size),
	}, nil
}

func (dir) Attr(ctx context.Context, a *fuse.Attr) error {
	// FIXME(inodes): a.Inode = 1
	a.Mode = os.ModeDir | 0o555
	a.Size = 2
	return nil
}

func (d dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	if d.tree == "" {
		// FIXME(sha): assumes SHA1
		if d.path == "" && len(name) == 40 {
			return dir{tree: name}, nil
		}
		dbg.Printf("Root lookup fell through: name=%v", name)
		return nil, syscall.ENOENT
	}
	node, err := gfs.nodefor(d.tree, d.path, name)
	if err == nil {
		dbg.PrintIf(2, "nodefor -> %#+v", node)
		return node, nil
	}
	log.Printf("Lookup fell through: dir=%v name=%v", d, name)
	return nil, syscall.ENOENT
}

func (d dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	if d.tree == "" {
		ents, err := gfs.rootlisting()
		if err != nil {
			log.Printf("rootlisting err: %v", err)
		}
		return ents, err
	}
	// FIXME(sha): assumes SHA1
	if len(d.tree) == 40 {
		dbg.PrintIf(2, "treelisting for %#+v", d)
		ents, err := gfs.treelisting(d.tree, d.path)
		if err != nil {
			log.Printf("treelisting err: %v", err)
		}
		return ents, err
	}
	log.Printf("ReadDirAll fell through: %#+v", d)
	return nil, syscall.ENOENT
}

func (f file) Attr(ctx context.Context, a *fuse.Attr) error {
	// FIXME(inodes): a.Inode = 2
	a.Mode = iofs.FileMode(f.mode)
	a.Size = f.size
	return nil
}

func (f file) ReadAll(ctx context.Context) ([]byte, error) {
	data, err := gfs.git("cat-file", "blob", f.id)
	if err != nil {
		return nil, fmt.Errorf("error getting object: %v", err)
	}
	return data, nil
}
