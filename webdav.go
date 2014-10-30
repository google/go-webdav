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
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-webdav/cond"
	x "github.com/google/go-webdav/xml"
)

// WebDAV is a http.Handler implementation that implements the WebDAV
// protocol over an abstract FileSystem. Set the Debug field to true
// in order to enable both serialization and logging of all requests.
type WebDAV struct {
	fs    FileSystem
	lm    *lockmaster
	m     sync.Mutex
	Debug bool
}

// NewWebDAV creates a WebDAV http.Handler wrapper around a given FileSystem.
func NewWebDAV(fs FileSystem) *WebDAV {
	return &WebDAV{
		fs: fs,
		lm: newLockMaster(),
	}
}

// fsEnv implements cond.Env, without exposing it via WebDAV
type fsEnv struct {
	w *WebDAV
}

func (e fsEnv) ETag(r string) string {
	p, err := e.w.fs.ForPath(r)
	if err != nil {
		return ""
	}
	f, err := p.Lookup()
	if err != nil {
		return ""
	}
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	return etag(fi)
}

func (e fsEnv) Locked(r, l string) bool {
	lock := e.w.lm.isLocked(r, l)
	return lock
}

type context struct {
	p         Path
	depth     int
	timeout   time.Duration
	cond      *cond.IfTag
	overwrite bool
}

// requestDepth gets the desired depth from the given request, defaults
// to infinity if none specified.
func parseDepth(r *http.Request) (int, error) {
	dh := r.Header.Get("Depth")
	if dh == "infinity" || dh == "Infinity" || dh == "" {
		return -1, nil
	}
	d, err := strconv.Atoi(dh)
	if err != nil {
		return 0, ErrorBadDepth.WithCause(err)
	}
	if d < 0 {
		return 0, ErrorBadDepth.WithCause(
			errors.New("depth must be non-negative or infinity"))
	}
	return d, nil
}

// requestTimeout gets the desired timeout from the request, defaults
// to one second if none specified or if invalid.
func parseTimeout(r *http.Request) time.Duration {
	// Only consider the first 3 presented options.
	// Spec permits us to ignore this header, so we're free to do
	// this if we wish (limits potential processing).
	opts := strings.SplitN(r.Header.Get("Timeout"), ",", 3)
	for _, o := range opts {
		o = strings.TrimSpace(o)
		if o == "Infinite" {
			// We ignore the infinite request
			continue
		}
		o = strings.TrimPrefix("Second-", o)
		d, err := strconv.Atoi(o)
		if err != nil {
			// Ignoring invalid.
			continue
		}
		return time.Duration(d) * time.Second
	}
	return time.Second
}

func parseIfHeader(r *http.Request) (*cond.IfTag, error) {
	ih := r.Header.Get("If")
	if ih == "" {
		return nil, nil
	}
	t, err := cond.ParseIfTag(ih)
	if err != nil {
		return nil, err
	}
	err = t.RewriteHosts(r.Host)
	if err != nil {
		return nil, err
	}
	log.Printf("If %s", t)
	return t, nil
}

func (s *WebDAV) extractContext(r *http.Request) (ctx context, err error) {
	ctx.p, err = s.fs.ForPath(r.URL.Path)
	if err != nil {
		return
	}

	ctx.depth, err = parseDepth(r)
	if err != nil {
		return
	}

	ctx.cond, err = parseIfHeader(r)
	if err != nil {
		return
	}

	ctx.timeout = parseTimeout(r)
	ctx.overwrite = r.Header.Get("Overwrite") != "F"
	return
}

func (s *WebDAV) checkCanWrite(ctx context, p Path) bool {
	l := s.lm.getLockForPath(p.String())
	if l == nil {
		return true
	}
	if ctx.cond == nil {
		return false
	}
	tokens := ctx.cond.GetAllTokens()
	for _, t := range tokens {
		if s.lm.isLocked(p.String(), t) {
			return true
		}
	}
	return false
}

func (s *WebDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Debug processing, force serialization of all requests and
	// log their details.
	if s.Debug {
		s.m.Lock()
		defer s.m.Unlock()

		log.Println()
		log.Println(r.Method, r.URL)
		for k, v := range r.Header {
			log.Println(k, ":", v)
		}
	}

	// Handle dumping all files.
	if r.URL.Path == "/dumpz" {
		s.fs.Dumpz()
		return
	}

	ctx, err := s.extractContext(r)
	if err != nil {
		s.errorHeader(ctx, w, err)
		return
	}

	if ctx.cond != nil {
		if !ctx.cond.Eval(fsEnv{w: s}, ctx.p.String()) {
			log.Println("Precondition failed")
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
	}

	switch r.Method {
	case "OPTIONS":
		s.doOptions(ctx, w, r)

	case "GET":
		s.doGet(ctx, w, r)
	case "HEAD":
		s.doHead(ctx, w, r)
	case "POST":
		s.doPost(ctx, w, r)
	case "DELETE":
		s.doDelete(ctx, w, r)
	case "PUT":
		s.doPut(ctx, w, r)
	case "MKCOL":
		s.doMkcol(ctx, w, r)

	case "COPY":
		s.doCopy(ctx, w, r)
	case "MOVE":
		s.doMove(ctx, w, r)

	case "PROPFIND":
		s.doPropfind(ctx, w, r)
	case "PROPPATCH":
		s.doProppatch(ctx, w, r)

	case "LOCK":
		s.doLock(ctx, w, r)
	case "UNLOCK":
		s.doUnlock(ctx, w, r)

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (s *WebDAV) allowedHeader(w http.ResponseWriter, p Path) {
	allowed := "OPTIONS, MKCOL, PUT, LOCK"
	f, err := p.Lookup()
	if err == nil {
		allowed = "OPTIONS, GET, HEAD, POST, DELETE, TRACE, PROPPATCH, COPY, MOVE, LOCK, UNLOCK"
		if f.IsDirectory() {
			allowed += ", PUT, PROPFIND"
		}
	}
	w.Header().Set("Allow", allowed)
}

func (s *WebDAV) errorHeader(ctx context, w http.ResponseWriter, e error) {
	log.Printf("E[%s]: %s", ctx.p, e)
	if we, ok := e.(Error); ok {
		w.WriteHeader(we.HTTPCode())
		if we.HTTPCode() == http.StatusMethodNotAllowed {
			s.allowedHeader(w, ctx.p)
		}
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *WebDAV) doOptions(ctx context, w http.ResponseWriter, r *http.Request) {
	// http://www.webdav.org/specs/rfc4918.html#dav.compliance.classes
	w.Header().Set("DAV", "1, 2")
	s.allowedHeader(w, ctx.p)
	w.Header().Set("MS-Author-Via", "DAV")
}

// http://www.webdav.org/specs/rfc4918.html#rfc.section.9.4
func (s *WebDAV) doGet(ctx context, w http.ResponseWriter, r *http.Request) {
	s.servePath(ctx, w, r, true)
}

// http://www.webdav.org/specs/rfc4918.html#rfc.section.9.4
func (s *WebDAV) doHead(ctx context, w http.ResponseWriter, r *http.Request) {
	s.servePath(ctx, w, r, false)
}

func (s *WebDAV) servePath(ctx context, w http.ResponseWriter, r *http.Request, content bool) {
	f, err := ctx.p.Lookup()
	if err != nil {
		s.errorHeader(ctx, w, ErrorNotFound.WithCause(err))
		return
	}

	fi, err := f.Stat()
	if err != nil {
		s.errorHeader(ctx, w, err)
		return
	}
	var fh FileHandle
	if content {
		fh, err = f.Open()
	} else {
		fh = &emptyFile{}
	}
	if err != nil {
		s.errorHeader(ctx, w, err)
		return
	}
	defer fh.Close()
	w.Header().Set("ETag", etag(fi))
	http.ServeContent(w, r, ctx.p.String(), fi.LastModified, fh)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_POST
func (s *WebDAV) doPost(ctx context, w http.ResponseWriter, r *http.Request) {
	s.doGet(ctx, w, r)
}

// http://www.wbdav.org/specs/rfc4918.html#METHOD_DELETE
func (s *WebDAV) doDelete(ctx context, w http.ResponseWriter, r *http.Request) {
	if !s.checkCanWrite(ctx, ctx.p) {
		s.errorHeader(ctx, w, ErrorLocked)
		return
	}

	f, err := ctx.p.Lookup()
	if err != nil {
		s.errorHeader(ctx, w, err)
		return
	}

	if !f.IsDirectory() {
		err = ctx.p.Remove()
		if err != nil {
			s.errorHeader(ctx, w, err)
			return
		}
		return
	}

	errs := ctx.p.RecursiveRemove()
	if len(errs) == 0 {
		w.WriteHeader(http.StatusNoContent)
	} else {
		ms := x.NewMultiStatus()
		for p, e := range errs {
			ms.AddStatus(p, e)
		}
		ms.Send(w)
	}
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_PUT
func (s *WebDAV) doPut(ctx context, w http.ResponseWriter, r *http.Request) {
	if !s.checkCanWrite(ctx, ctx.p) {
		s.errorHeader(ctx, w, ErrorLocked)
		return
	}

	var fh FileHandle
	f, err := ctx.p.Lookup()
	exists := false
	if err == nil {
		if f.IsDirectory() {
			s.errorHeader(ctx, w, ErrorIsDir)
			return
		}

		exists = true
		fh, err = f.Truncate()
	} else {
		f, fh, err = ctx.p.Create()
	}

	if err != nil {
		s.errorHeader(ctx, w, ErrorConflict.WithCause(err))
		return
	}
	defer fh.Close()

	if _, err := io.Copy(fh, r.Body); err != nil {
		s.errorHeader(ctx, w, ErrorConflict)
	} else {
		if exists {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
	}
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_MKCOL
func (s *WebDAV) doMkcol(ctx context, w http.ResponseWriter, r *http.Request) {
	if !s.checkCanWrite(ctx, ctx.p) {
		s.errorHeader(ctx, w, ErrorLocked)
		return
	}

	_, err := ctx.p.Lookup()
	if err == nil {
		s.errorHeader(ctx, w, ErrorNotAllowed)
		return
	}

	if r.ContentLength > 0 {
		s.errorHeader(ctx, w, ErrorUnsupportedType)
		return
	}

	_, err = ctx.p.Mkdir()
	if err != nil {
		s.errorHeader(ctx, w, ErrorConflict.WithCause(err))
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_COPY
func (s *WebDAV) doCopy(ctx context, w http.ResponseWriter, r *http.Request) {
	s.handleCopyOrMove(ctx, w, r, false)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_MOVE
func (s *WebDAV) doMove(ctx context, w http.ResponseWriter, r *http.Request) {
	s.handleCopyOrMove(ctx, w, r, true)
}

func (s *WebDAV) handleCopyOrMove(ctx context, w http.ResponseWriter, r *http.Request, move bool) {
	src := ctx.p
	if move && !s.checkCanWrite(ctx, src) {
		s.errorHeader(ctx, w, ErrorLocked)
		return
	}

	dhdr := r.Header.Get("Destination")
	if dhdr == "" {
		s.errorHeader(ctx, w, ErrorBadDest)
		return
	}
	durl, err := url.Parse(dhdr)
	if err != nil {
		s.errorHeader(ctx, w, ErrorBadDest.WithCause(err))
		return
	}

	// Destination host must match our source.
	if durl.Host != r.Host {
		s.errorHeader(ctx, w, ErrorBadHost)
		return
	}

	dst, err := s.fs.ForPath(durl.Path)
	if err != nil {
		s.errorHeader(ctx, w, ErrorBadDest.WithCause(err))
		return
	}

	if !s.checkCanWrite(ctx, dst) {
		s.errorHeader(ctx, w, ErrorLocked)
		return
	}

	log.Println("TO ", dst)
	newf, err := src.CopyTo(dst, CopyOptions{
		Overwrite: ctx.overwrite,
		Move:      move,
		Depth:     ctx.depth,
	})
	if err != nil {
		s.errorHeader(ctx, w, err)
		return
	}
	if newf {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

var fileStatProps = map[string]bool{
	"DAV::getlastmodified":  true,
	"DAV::getetag":          true,
	"DAV::getcontentlength": true,
	"DAV::creationdate":     true,
}

func etag(fi FileInfo) string {
	return fmt.Sprintf("%d-%s", fi.Size, fi.LastModified)
}

func getFileStatProp(n string, f File) (v string, err error) {
	fi, err := f.Stat()
	if err != nil {
		return
	}
	switch n {
	case "DAV::getlastmodified":
		v = fi.LastModified.String()
	case "DAV::getetag":
		v = etag(fi)
	case "DAV::getcontentlength":
		v = strconv.FormatInt(fi.Size, 10)
	case "DAV::creationdate":
		v = fi.Created.String()
	}
	return
}

// getPropValue gets a property for a given file, potentially generating
// synthetic properties that are expected. It will always return a value
// with the correct name, but potentially lack a value if not present.
func (s *WebDAV) getPropValue(pn string, f File) (x.Any, bool) {
	a := x.NewAny(pn)
	switch pn {
	case "DAV::resourcetype":
		if f.IsDirectory() {
			a.Inner = "<collection xmlns=\"DAV:\"/>"
		}
		return a, true
	case "DAV::supportedlock":
		a.Inner = `
<D:lockentry xmlns:D="DAV::">
<D:lockscope><D:exclusive/></D:lockscope>
<D:locktype><D:write/></D:locktype>
</D:lockentry>`
		return a, true
	case "DAV::lockdiscovery":
		l := s.lm.getLockForPath(f.GetPath())
		if l != nil {
			a.Inner = l.toXML()
		}
		return a, true
	case "DAV::displayname":
		a.Value = path.Base(f.GetPath())
		return a, true
	}

	if fileStatProps[pn] {
		v, err := getFileStatProp(pn, f)
		if err != nil {
			return a, false
		}
		a.Value = v
		return a, true
	}
	v, ok := f.GetProp(pn)
	a.Value = v
	return a, ok
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_PROPFIND
func (s *WebDAV) doPropfind(ctx context, w http.ResponseWriter, r *http.Request) {
	// TODO(nmvc): Limit request size.
	req, err := x.ParsePropFind(r.Body)
	if err != nil {
		s.errorHeader(ctx, w, ErrorBadPropfind.WithCause(err))
		return
	}

	files, err := ctx.p.LookupSubtree(ctx.depth)
	if err != nil {
		s.errorHeader(ctx, w, err)
		return
	}
	log.Printf("FOUND %d files", len(files))

	ms := x.NewMultiStatus()
	for _, f := range files {
		var found, missing []x.Any
		for _, pn := range req.PropertyNames {
			v, ok := s.getPropValue(pn, f)
			if ok {
				found = append(found, v)
			} else {
				missing = append(missing, v)
			}
		}
		ms.AddPropStatus(f.GetPath(), found, missing)
	}
	ms.Send(w)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_PROPPATCH
func (s *WebDAV) doProppatch(ctx context, w http.ResponseWriter, r *http.Request) {
	if !s.checkCanWrite(ctx, ctx.p) {
		s.errorHeader(ctx, w, ErrorLocked)
		return
	}

	f, err := ctx.p.Lookup()
	if err != nil {
		s.errorHeader(ctx, w, err)
		return
	}

	// TODO(nmvc): Limit request size.
	req, err := x.ParsePropPatch(r.Body)
	if err != nil {
		s.errorHeader(ctx, w, ErrorBadProppatch.WithCause(err))
		return
	}

	err = f.PatchProp(req.Set, req.Remove)
	if err != nil {
		s.errorHeader(ctx, w, ErrorConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_LOCK
func (s *WebDAV) doLock(ctx context, w http.ResponseWriter, r *http.Request) {
	req, err := x.ParseLock(r.Body)
	if err != nil {
		s.errorHeader(ctx, w, ErrorBadLock.WithCause(err))
		return
	}
	log.Printf("REQ %+v", req)

	// We don't let you lock on anything without a parent.
	_, err = ctx.p.Parent().Lookup()
	if err != nil {
		s.errorHeader(ctx, w, ErrorMissingParent)
		return
	}

	var l *lock
	if req.Refresh {
		if ctx.cond == nil {
			s.errorHeader(ctx, w, ErrorBadLock)
			return
		}
		tok, ok := ctx.cond.GetSingleState()
		if !ok {
			s.errorHeader(ctx, w, ErrorBadLock)
			return
		}
		l, err = s.lm.refreshLock(tok, ctx.p, ctx.timeout)
	} else {
		l, err = s.lm.createLock(req.Owner, ctx.p, ctx.depth, ctx.timeout)
	}
	if err != nil {
		s.errorHeader(ctx, w, err)
		return
	}

	if !req.Refresh {
		w.Header().Set("Lock-Token", "<"+l.token+">")
	}

	// Now that we have a successful lock, create the resource
	// if it didn't exist already.
	_, err = ctx.p.Lookup()
	if err != nil {
		_, fh, err := ctx.p.Create()
		if err != nil {
			// Unlock, as we're failing.
			s.lm.unlock(l.token)
			s.errorHeader(ctx, w, err)
			return
		}
		fh.Close()
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	log.Println(l)

	a := x.NewAny("DAV::lockdiscovery")
	a.Inner = l.toXML()
	x.SendProp(a, w)
}

// http://www.webdav.org/specs/rfc4918.html#METHOD_UNLOCK
func (s *WebDAV) doUnlock(ctx context, w http.ResponseWriter, r *http.Request) {
	lt := r.Header.Get("Lock-Token")
	if len(lt) > 2 && lt[0] == '<' {
		lt = lt[1 : len(lt)-1]
	}

	if !s.lm.isLocked(ctx.p.String(), lt) {
		s.errorHeader(ctx, w, ErrorBadLock)
		return
	}
	s.lm.unlock(lt)
}
