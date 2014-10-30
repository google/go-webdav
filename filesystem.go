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

package webdav

import (
	"io"
	"time"
)

// FileSystem represents and abstract filesystem that can perform
// operations on paths.
type FileSystem interface {
	ForPath(p string) (Path, error)
	Dumpz()
}

type CopyOptions struct {
	Overwrite, Move bool
	Depth           int
}

// Path is a unique path in the filesystem.
type Path interface {
	String() string
	Parent() Path
	Lookup() (File, error)
	LookupSubtree(depth int) ([]File, error)
	Mkdir() (File, error)
	Create() (File, FileHandle, error)
	CopyTo(dst Path, opt CopyOptions) (bool, error)
	Remove() error
	RecursiveRemove() map[string]error
}

// FileInfo represents all metadat about a File.
type FileInfo struct {
	Created, LastModified time.Time
	Size                  int64
}

// File represents an abstract File (or directory)
type File interface {
	GetPath() string
	IsDirectory() bool
	Stat() (FileInfo, error)
	Open() (FileHandle, error)
	Truncate() (FileHandle, error)
	PatchProp(set, remove map[string]string) error
	GetProp(k string) (string, bool)
}

// FileHandle is an open reference to a file for writing or reading.
type FileHandle interface {
	io.ReadSeeker
	io.Closer
	io.Writer
}

// emptyFile represents an empty file, it also implements FileHandle
type emptyFile struct{}

var _ FileHandle = &emptyFile{}

func (e *emptyFile) Write(b []byte) (int, error) {
	return 0, io.EOF
}

func (e *emptyFile) Close() error {
	return nil
}

func (e *emptyFile) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (e *emptyFile) Seek(offset int64, whence int) (ret int64, err error) {
	return 0, nil
}
