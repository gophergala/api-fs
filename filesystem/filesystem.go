package filesystem

import (
	"log"
	"os"
	"sync"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

type FS struct {
	rootDir *RootDir
}

func NewFS() *FS {
	resource := newResourceDir(1, "www.example.com")
	dirs := []*ResourceDir{resource}
	dirMap := map[string]int{
		"www.example.com": 0,
	}

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
}

func (d *RootDir) Attr() fuse.Attr {
	return fuse.Attr{
		Inode: 1,
		Mode:  os.ModeDir | 0555,
	}
}

func (d *RootDir) ReadDir(intr fs.Intr) ([]fuse.Dirent, fuse.Error) {
	ents := make([]fuse.Dirent, len(d.dirs))

	for i, d := range d.dirs {
		ents[i].Name = d.name
		ents[i].Type = fuse.DT_Dir
	}

	return ents, nil
}

func (d *RootDir) Lookup(name string, intr fs.Intr) (fs.Node, fuse.Error) {
	if n, ok := d.dirMap[name]; ok {
		log.Printf("returning %d (%#v)", n, d.dirs[n])
		return d.dirs[n], nil
	}

	return nil, fuse.ENOENT
}

// ResourceDir represents an HTTP API resource directory.
type ResourceDir struct {
	name    string
	inode   uint64
	ctlFile *ControlFile
}

func newResourceDir(parent uint64, name string) *ResourceDir {
	inode := fs.GenerateDynamicInode(parent, name)

	return &ResourceDir{
		name:    name,
		inode:   inode,
		ctlFile: newControlFile(inode),
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
	default:
		err = fuse.ENOENT
	}

	return n, err
}

// ControlFile wraps around a []byte, doing syntax checking on write.
type ControlFile struct {
	data  []byte
	inode uint64
	m     sync.Mutex
}

func newControlFile(parent uint64) *ControlFile {
	return &ControlFile{
		data:  []byte("hello, world!\n"),
		inode: parent,
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
		return f.data[i:], nil
	}

	return f.data[i:lastAddr], nil
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
		f.data = b
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
	f  *ControlFile
	id fuse.HandleID
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

	u = fuse.HandleID(f.inode)

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

	return nil
}

func (h *controlHandle) Flush(req *fuse.FlushRequest,
	intr fs.Intr) fuse.Error {

	log.Printf("Flush %d", h.id)

	return nil
}

func (h *controlHandle) Release(req *fuse.ReleaseRequest,
	intr fs.Intr) fuse.Error {

	log.Printf("Release %d", h.id)

	return nil
}
