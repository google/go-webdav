// Copyright 2014 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package memfs is an in-memory implementation of webdav.FileSystem. It has no
limits on how much memory it will consume for files, is recommended solely for
testing purposes.
*/
package memfs

import (
	"io"
	"log"
	"path"
	"sort"
	"sync"
	"time"

	w "github.com/google/go-webdav"
	wp "github.com/google/go-webdav/path"
)

type memfs struct {
	m     sync.Mutex
	files map[string]*memfile
}

// NewMemFS creates a new webdav.FileSystem based in memory.
func NewMemFS() w.FileSystem {
	fs := &memfs{files: make(map[string]*memfile)}
	fs.files["/"] = newMemFile(fs, "/", true)
	return fs
}

func (fs *memfs) Dumpz() {
	log.Printf("dump:")
	n := make([]string, 0, len(fs.files))
	for k := range fs.files {
		n = append(n, k)
	}
	sort.StringSlice(n).Sort()
	for _, k := range n {
		log.Printf("%s", k)
	}
}

func (fs *memfs) ForPath(p string) (w.Path, error) {
	p = path.Clean(p)
	if !path.IsAbs(p) {
		return nil, w.ErrorBadPath
	}
	return &memp{fs: fs, path: p}, nil
}

type memp struct {
	fs   *memfs
	path string
}

func (p *memp) String() string {
	return p.path
}

func (p *memp) Parent() w.Path {
	return p.parent()
}

func (p *memp) parent() *memp {
	return &memp{fs: p.fs, path: path.Dir(p.path)}
}

func (p *memp) internalLookup() (*memfile, error) {
	f, ok := p.fs.files[p.path]
	if !ok {
		return nil, w.ErrorNotFound
	}
	return f, nil
}

func (p *memp) Lookup() (w.File, error) {
	p.fs.m.Lock()
	defer p.fs.m.Unlock()
	return p.internalLookup()
}

func (p *memp) LookupSubtree(depth int) ([]w.File, error) {
	_, err := p.Lookup()
	if err != nil {
		return nil, err
	}

	p.fs.m.Lock()
	defer p.fs.m.Unlock()

	var files []w.File
	for fn, f := range p.fs.files {
		if _, ok := wp.Included(fn, p.path, depth); ok {
			files = append(files, f)
		}
	}
	return files, nil
}

func (p *memp) Mkdir() (w.File, error) {
	if _, err := p.Lookup(); err == nil {
		return nil, w.ErrorConflict
	}
	p.fs.m.Lock()
	defer p.fs.m.Unlock()
	if _, err := p.parent().internalLookup(); err != nil {
		return nil, w.ErrorMissingParent
	}

	f := newMemFile(p.fs, p.path, true)
	p.fs.files[p.path] = f
	return f, nil
}

func (p *memp) Create() (w.File, w.FileHandle, error) {
	if _, err := p.Lookup(); err == nil {
		return nil, nil, w.ErrorConflict
	}
	p.fs.m.Lock()
	defer p.fs.m.Unlock()
	if _, err := p.parent().internalLookup(); err != nil {
		return nil, nil, w.ErrorMissingParent
	}

	f := newMemFile(p.fs, p.path, false)
	p.fs.files[p.path] = f
	fh, err := f.Open()
	return f, fh, err
}

func (p *memp) Remove() error {
	p.fs.m.Lock()
	defer p.fs.m.Unlock()
	f, err := p.internalLookup()
	if err != nil {
		return w.ErrorNotFound
	} else if f.IsDirectory() {
		return w.ErrorIsDir
	}
	delete(p.fs.files, f.path)
	return nil
}

func (p *memp) removeSubtree(subtree string) {
	log.Println("RST", subtree)
	for path := range p.fs.files {
		if wp.InTree(path, subtree) {
			delete(p.fs.files, path)
		}
	}
}

func (p *memp) RecursiveRemove() (errs map[string]error) {
	p.fs.m.Lock()
	defer p.fs.m.Unlock()
	f, err := p.internalLookup()
	errs = make(map[string]error)
	if err != nil {
		errs[p.path] = w.ErrorNotFound
		return
	} else if !f.IsDirectory() {
		errs[f.path] = w.ErrorIsNotDir
		return
	}
	p.removeSubtree(f.path)
	return
}

func (p *memp) CopyTo(dst w.Path, opt w.CopyOptions) (bool, error) {
	p.fs.m.Lock()
	defer p.fs.m.Unlock()

	dstp, ok := dst.(*memp)
	if !ok {
		return false, w.ErrorBadHost
	}

	if p.path == dstp.path {
		return false, w.ErrorSameFile
	}

	srcf, err := p.internalLookup()
	if err != nil {
		return false, w.ErrorNotFound
	}

	// Can only move complete directory trees.
	if srcf.IsDirectory() && opt.Move && opt.Depth >= 0 {
		return false, w.ErrorIsDir
	}

	if _, err := dstp.parent().internalLookup(); err != nil {
		return false, w.ErrorMissingParent
	}

	newf := true
	_, err = dstp.internalLookup()
	if err == nil {
		if opt.Overwrite {
			newf = false
			p.removeSubtree(dstp.path)
		} else {
			return false, w.ErrorDestExists
		}
	}

	for orig, v := range p.fs.files {
		nn, ok := wp.Included(orig, p.path, opt.Depth)
		if !ok {
			continue
		}
		nn = path.Join(dstp.path, nn)
		if opt.Move {
			log.Printf("MOVE %s -> %s", orig, nn)
			// Note: As we adjust path here, it is important that
			// this move operation does not depend on anything
			// file-related.
			v.path = nn
			p.fs.files[nn] = v
			delete(p.fs.files, orig)
		} else {
			log.Printf("COPY %s -> %s", orig, nn)
			nv := v.clone(nn)
			p.fs.files[nn] = nv
		}
	}
	return newf, nil
}

type memfile struct {
	fs   *memfs
	dir  bool
	path string
	i    w.FileInfo

	m    sync.Mutex
	data []byte
	p    map[string]string
}

func newMemFile(fs *memfs, path string, dir bool) *memfile {
	var d []byte
	if !dir {
		d = make([]byte, 0)
	}
	return &memfile{
		fs:   fs,
		dir:  dir,
		path: path,
		p:    make(map[string]string),
		i:    w.FileInfo{Created: time.Now()},
		data: d,
	}
}

func (f *memfile) clone(np string) *memfile {
	f.m.Lock()
	defer f.m.Unlock()

	mf := newMemFile(f.fs, np, f.dir)
	if !f.dir {
		mf.data = make([]byte, len(f.data))
		copy(mf.data, f.data)
	}
	for k, v := range f.p {
		mf.p[k] = v
	}
	return mf
}

func (f *memfile) GetPath() string {
	return f.path
}

func (f *memfile) PatchProp(set, remove map[string]string) error {
	f.m.Lock()
	defer f.m.Unlock()
	for k, v := range set {
		f.p[k] = v
	}
	for k := range remove {
		delete(f.p, k)
	}
	return nil
}

func (f *memfile) GetProp(k string) (string, bool) {
	f.m.Lock()
	defer f.m.Unlock()
	_, exists := f.p[k]
	return f.p[k], exists
}

func (f *memfile) IsDirectory() bool {
	return f.dir
}

func (f *memfile) Stat() (w.FileInfo, error) {
	f.m.Lock()
	defer f.m.Unlock()
	f.i.Size = int64(len(f.data))
	return f.i, nil
}

func (f *memfile) Open() (w.FileHandle, error) {
	f.m.Lock()
	defer f.m.Unlock()
	if f.dir {
		return nil, w.ErrorIsDir
	}
	if f.data == nil {
		return nil, w.ErrorNotFound
	}
	return &memfileh{f: f}, nil
}

func (f *memfile) Truncate() (w.FileHandle, error) {
	f.m.Lock()
	defer f.m.Unlock()
	if f.dir {
		return nil, w.ErrorIsDir
	}
	f.data = make([]byte, 0)
	f.i.LastModified = time.Now()
	return &memfileh{f: f}, nil
}

type memfileh struct {
	f   *memfile
	pos int64
}

func (h *memfileh) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	h.f.m.Lock()
	defer h.f.m.Unlock()

	start := int(h.pos)
	end := start + len(b)
	log.Println("Write", len(b), start, end)
	if end > len(h.f.data) {
		// Resize the in-memory portion to accomodate the write.
		old := h.f.data
		h.f.data = make([]byte, end)
		copy(h.f.data, old)
	}
	copy(h.f.data[start:end], b)
	h.pos = int64(end)
	h.f.i.LastModified = time.Now()
	return len(b), nil
}

func (h *memfileh) Close() error {
	return nil
}

func (h *memfileh) Read(p []byte) (int, error) {
	h.f.m.Lock()
	defer h.f.m.Unlock()

	start := int(h.pos)
	if start >= len(h.f.data) {
		return 0, io.EOF
	}

	end := start + len(p)
	if end > len(h.f.data) {
		end = len(h.f.data)
	}
	log.Println("Read", len(p), start, end)
	n := copy(p, h.f.data[h.pos:end])
	h.pos = int64(end)
	return n, nil
}

func (h *memfileh) Seek(offset int64, whence int) (int64, error) {
	h.f.m.Lock()
	defer h.f.m.Unlock()
	np := h.pos
	if whence == 0 {
		np = offset
	} else if whence == 1 {
		np += offset
	} else if whence == 2 {
		np = int64(len(h.f.data)) + offset
	}
	if np < 0 {
		return h.pos, w.ErrorUnderrun
	}
	h.pos = np
	return h.pos, nil
}
