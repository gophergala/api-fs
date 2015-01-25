package filesystem

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"sync"

	"github.com/gophergala/api-fs/api"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

type FS struct {
	rootDir *RootDir
}

func NewFS() *FS {
	dirs := []*ResourceDir{}
	dirMap := map[string]int{}

	rootDir := &RootDir{
		dirs:   dirs,
		dirMap: dirMap,
	}

	return &FS{
		rootDir: rootDir,
	}
}

func (f *FS) Root() (fs.Node, fuse.Error) {
	return f.rootDir, nil
}

// RootDir represents the root directory of api-fs.
type RootDir struct {
	dirs   []*ResourceDir
	dirMap map[string]int
	sync.Mutex
}

func (d *RootDir) Attr() fuse.Attr {
	return fuse.Attr{
		Inode: 1,
		Mode:  os.ModeDir | 0777,
	}
}

func (d *RootDir) ReadDir(intr fs.Intr) ([]fuse.Dirent, fuse.Error) {
	d.Lock()
	defer d.Unlock()

	ents := make([]fuse.Dirent, len(d.dirs))

	for i, d := range d.dirs {
		ents[i].Name = d.name
		ents[i].Type = fuse.DT_Dir
	}

	return ents, nil
}

func (d *RootDir) Lookup(name string, intr fs.Intr) (fs.Node, fuse.Error) {
	d.Lock()
	defer d.Unlock()

	if n, ok := d.dirMap[name]; ok {
		log.Printf("returning %d (%#v)", n, d.dirs[n])
		return d.dirs[n], nil
	}

	return nil, fuse.ENOENT
}

func (d *RootDir) Mkdir(req *fuse.MkdirRequest, intr fs.Intr) (fs.Node, fuse.Error) {
	d.Lock()
	defer d.Unlock()

	log.Printf("Mkdir %d %s", 1, req.Name)
	if _, ok := d.dirMap[req.Name]; ok {
		return nil, fuse.EEXIST
	}

	n := newResourceDir(1, req.Name, "")
	d.dirs = append(d.dirs, n)
	d.dirMap[req.Name] = len(d.dirs) - 1

	return n, nil
}

// ResourceDir represents an HTTP API resource directory.
type ResourceDir struct {
	name     string
	fullpath string
	inode    uint64
	ctlFile  *ControlFile
	bodyFile *bodyFile
}

func newResourceDir(parent uint64, name string, parentPath string) *ResourceDir {
	inode := fs.GenerateDynamicInode(parent, name)
	fullpath := fmt.Sprintf("%s/%s", parentPath, name)
	ctl := newControlFile(inode, fullpath)
	body := newBodyFile(inode, ctl)
	return &ResourceDir{
		name:     name,
		fullpath: fullpath,
		inode:    inode,
		ctlFile:  ctl,
		bodyFile: body,
	}
}

func (d *ResourceDir) Attr() fuse.Attr {
	log.Printf("%d getting attributes", d.inode)
	attr := fuse.Attr{
		Inode: d.inode,
		Mode:  os.ModeDir | 0555,
	}

	return attr
}

func (d *ResourceDir) ReadDir(intr fs.Intr) ([]fuse.Dirent, fuse.Error) {
	log.Printf("%d: reading dir", d.inode)

	dirs := []fuse.Dirent{
		{
			Name: "ctl",
			Type: fuse.DT_File,
		},
		{
			Name: "body",
			Type: fuse.DT_File,
		},
	}

	return dirs, nil
}

func (d *ResourceDir) Lookup(name string, intr fs.Intr) (fs.Node, fuse.Error) {
	var (
		n   fs.Node
		err fuse.Error
	)

	switch name {
	case "ctl":
		n = d.ctlFile
	case "body":
		n = d.bodyFile
	default:
		err = fuse.ENOENT
	}

	return n, err
}

// ControlFile wraps around a []byte, doing syntax checking on write.
type ControlFile struct {
	url   string
	data  []byte
	inode uint64
	m     sync.Mutex
	ready chan api.Params
}

func newControlFile(parent uint64, fullpath string) *ControlFile {
	return &ControlFile{
		data:  []byte("hello, world!\n"),
		inode: parent,
		ready: make(chan api.Params),
		url:   fmt.Sprintf("http:/%s", fullpath),
	}
}

func (f *ControlFile) Attr() fuse.Attr {
	attr := fuse.Attr{
		Inode: f.inode,
		Size:  uint64(len(f.data)),
		Mode:  0777,
	}

	return attr
}

func (f *ControlFile) readAt(i int64, n int) ([]byte, error) {
	f.m.Lock()
	defer f.m.Unlock()

	size := int64(len(f.data))

	if i >= size {
		return nil, fuse.ERANGE
	}

	lastAddr := i + int64(n)

	if lastAddr >= size {
		log.Printf("Read %d %s", f.inode, string(f.data))
		ret := make([]byte, size-i)
		copy(ret, f.data[i:])
		return ret, nil
	}

	ret := make([]byte, lastAddr-i)
	copy(ret, f.data[i:lastAddr])

	return ret, nil
}

func (f *ControlFile) writeAt(b []byte, i int64) (n int, err error) {
	f.m.Lock()
	defer f.m.Unlock()

	// Get total size ahead of time
	totalSize := int64(len(b)) + i

	switch {
	case i > int64(len(f.data))+1:
		return 0, fuse.ERANGE
	case i == 0:
		f.data = make([]byte, len(b))
		copy(f.data, b)
		return len(b), nil
	default:
		if int64(len(f.data)) < totalSize {
			newData := make([]byte, totalSize)
			copy(newData, f.data)
			f.data = newData
		}

		// Write!
		iInt := int(i)
		copy(f.data[iInt:len(b)+iInt], b)

		return len(b), nil
	}
}

func (f *ControlFile) Fsync(req *fuse.FsyncRequest, intr fs.Intr) fuse.Error {

	log.Printf("Fsync %d", f.inode)

	return nil
}

type controlHandle struct {
	f       *ControlFile
	id      fuse.HandleID
	isWrite bool
}

func (f *ControlFile) Open(req *fuse.OpenRequest, resp *fuse.OpenResponse,
	intr fs.Intr) (fs.Handle, fuse.Error) {

	openFlagStr := ""

	switch {
	case req.Flags&fuse.OpenReadOnly == fuse.OpenReadOnly:
		openFlagStr = openFlagStr + " OpenReadOnly"
	case req.Flags&fuse.OpenWriteOnly == fuse.OpenWriteOnly:
		openFlagStr = openFlagStr + " OpenWriteOnly"
	case req.Flags&fuse.OpenReadWrite == fuse.OpenReadWrite:
		openFlagStr = openFlagStr + " OpenReadWrite"
	case req.Flags&fuse.OpenAppend == fuse.OpenAppend:
		openFlagStr = openFlagStr + " OpenAppend"
	case req.Flags&fuse.OpenCreate == fuse.OpenCreate:
		openFlagStr = openFlagStr + " OpenCreate"
	case req.Flags&fuse.OpenExclusive == fuse.OpenExclusive:
		openFlagStr = openFlagStr + " OpenExclusive"
	case req.Flags&fuse.OpenSync == fuse.OpenSync:
		openFlagStr = openFlagStr + " OpenSync"
	case req.Flags&fuse.OpenTruncate == fuse.OpenTruncate:
		openFlagStr = openFlagStr + " OpenTruncate"
	}

	log.Printf("Open %d %s %d", f.inode, openFlagStr, req.Node)

	var u fuse.HandleID

	u = fuse.HandleID(rand.Uint32()) + fuse.HandleID(rand.Uint32()<<32)

	h := &controlHandle{
		f:  f,
		id: u,
	}

	resp.Handle = u

	return h, nil
}

func (h *controlHandle) Read(req *fuse.ReadRequest,
	resp *fuse.ReadResponse, intr fs.Intr) fuse.Error {

	log.Printf("Read %d %d %d", h.id, req.Offset, req.Size)

	b, err := h.f.readAt(req.Offset, req.Size)
	if err != nil {
		return err
	}

	resp.Data = b

	return nil
}

func (h *controlHandle) ReadAll(intr fs.Intr) ([]byte, fuse.Error) {
	return h.f.readAt(0, len(h.f.data))
}

func (h *controlHandle) Write(req *fuse.WriteRequest,
	resp *fuse.WriteResponse, intr fs.Intr) fuse.Error {
	log.Printf("%d got request to write %d bytes from %d: %s", h.f.inode,
		len(req.Data), req.Offset, string(req.Data))

	log.Printf("Write %d %d %d", h.id, req.Offset, len(req.Data))

	_, err := h.f.writeAt(req.Data, req.Offset)
	if err != nil {
		log.Printf("Write %d failed: %s", h.id, err)
		return err
	}

	resp.Size = len(req.Data)

	log.Printf("Write %d complete: %#v", h.id, resp)

	h.isWrite = true

	return nil
}

func (h *controlHandle) Flush(req *fuse.FlushRequest,
	intr fs.Intr) fuse.Error {

	log.Printf("Flush %d", h.id)

	return nil
}

func (h *controlHandle) Release(req *fuse.ReleaseRequest,
	intr fs.Intr) fuse.Error {

	writeStr := " NOT"
	if h.isWrite {
		writeStr = ""
		params := api.Params{
			URL:    h.f.url,
			Method: "GET",
		}
		select {
		case h.f.ready <- params:
		default:
			log.Printf("Already closed %d", h.id)
		}
	}

	log.Printf("Release %d (is%s write)", h.id, writeStr)

	return nil
}

type bodyFile struct {
	cf    *ControlFile
	inode uint64
	body  []byte
	err   error
	url   string
	ready chan struct{}
}

func newBodyFile(parent uint64, f *ControlFile) *bodyFile {
	inode := fs.GenerateDynamicInode(parent, "body")

	b := &bodyFile{
		cf:    f,
		inode: inode,
		ready: make(chan struct{}),
	}

	go func() {
		// body will only execute from control file once
		p := <-f.ready

		log.Printf("ASYNC Read %d", inode)

		b.populate(p)
	}()

	return b
}

func (f *bodyFile) Attr() fuse.Attr {
	attr := fuse.Attr{
		Inode: f.inode,
		Size:  0,
		Mode:  0555,
	}

	return attr
}

func (f *bodyFile) populate(p api.Params) {
	var reader io.ReadCloser
	reader, f.err = api.DoRequest(p)
	if f.err != nil {
		return
	}
	defer reader.Close()

	f.body, f.err = ioutil.ReadAll(reader)

	close(f.ready)
}

type bodyHandle struct {
	f *bodyFile
	u fuse.HandleID
}

func (f *bodyFile) Open(req *fuse.OpenRequest, resp *fuse.OpenResponse,
	intr fs.Intr) (fs.Handle, fuse.Error) {

	openFlagStr := ""

	switch {
	case req.Flags&fuse.OpenReadOnly == fuse.OpenReadOnly:
		openFlagStr = openFlagStr + " OpenReadOnly"
	case req.Flags&fuse.OpenWriteOnly == fuse.OpenWriteOnly:
		openFlagStr = openFlagStr + " OpenWriteOnly"
	case req.Flags&fuse.OpenReadWrite == fuse.OpenReadWrite:
		openFlagStr = openFlagStr + " OpenReadWrite"
	case req.Flags&fuse.OpenAppend == fuse.OpenAppend:
		openFlagStr = openFlagStr + " OpenAppend"
	case req.Flags&fuse.OpenCreate == fuse.OpenCreate:
		openFlagStr = openFlagStr + " OpenCreate"
	case req.Flags&fuse.OpenExclusive == fuse.OpenExclusive:
		openFlagStr = openFlagStr + " OpenExclusive"
	case req.Flags&fuse.OpenSync == fuse.OpenSync:
		openFlagStr = openFlagStr + " OpenSync"
	case req.Flags&fuse.OpenTruncate == fuse.OpenTruncate:
		openFlagStr = openFlagStr + " OpenTruncate"
	}

	log.Printf("Open %d %s %d", f.inode, openFlagStr, req.Node)

	var u fuse.HandleID

	u = fuse.HandleID(rand.Uint32()) + fuse.HandleID(rand.Uint32()<<32)

	h := &bodyHandle{
		f: f,
		u: u,
	}

	resp.Handle = u
	resp.Flags = resp.Flags | fuse.OpenDirectIO

	return h, nil
}

func (h *bodyHandle) ReadAll(intr fs.Intr) ([]byte, fuse.Error) {
	select {
	case <-intr:
		log.Printf("interrupted!")
		return nil, fuse.EINTR
	case <-h.f.ready:
	}

	if h.f.err != nil {
		log.Printf("Read error: %s", h.f.err)
		return nil, fuse.EIO
	}

	return h.f.body, nil
}
