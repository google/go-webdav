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
	"math/rand"
	"strconv"
	"sync"
	"time"
	wp "webdav/path"
)

var (
	minLockDuration = 20 * time.Second
	maxLockDuration = 5 * time.Minute
)

type lock struct {
	token    string
	depth    int
	owner    string // vertabim XML
	duration time.Duration
	modified time.Time
	path     string
	m        sync.Mutex
}

func (l *lock) String() string {
	t := (l.duration - time.Since(l.modified))
	return fmt.Sprintf("%s@%d T%s D%s", l.path, l.depth, l.token, t)
}

func (l *lock) toXml() string {
	l.m.Lock()
	defer l.m.Unlock()
	ds := strconv.Itoa(l.depth)
	if l.depth < 0 {
		ds = "infinity"
	}

	t := (l.duration - time.Since(l.modified)) / time.Second
	return fmt.Sprintf(`
<activelock>
  <locktype><write/></locktype>
  <lockscope><exclusive/></lockscope>
  <depth>%s</depth>
  <owner>%s</owner>
  <timeout>Second-%d</timeout>
  <locktoken><href>%s</href></locktoken>
  <lockroot><href>%s</href></lockroot>
</activelock>`, ds, l.owner, t, l.token, wp.UrlEncode(l.path))
}

func (l *lock) touch() {
	l.m.Lock()
	defer l.m.Unlock()
	l.modified = time.Now()
}

func (l *lock) expired() bool {
	l.m.Lock()
	defer l.m.Unlock()
	return time.Now().After(l.modified.Add(l.duration))
}

type lockmaster struct {
	m     sync.Mutex
	locks map[string]*lock
}

func newLockMaster() *lockmaster {
	return &lockmaster{locks: make(map[string]*lock)}
}

func (lm *lockmaster) getLockForPath(p string) *lock {
	lm.m.Lock()
	defer lm.m.Unlock()
	for _, l := range lm.locks {
		if l.expired() {
			delete(lm.locks, l.token)
			continue
		}

		if _, ok := wp.Included(p, l.path, l.depth); !ok {
			continue
		}
		return l
	}
	return nil
}

func (lm *lockmaster) isLocked(p, t string) bool {
	lm.m.Lock()
	defer lm.m.Unlock()
	l := lm.locks[t]
	if l == nil || l.expired() {
		delete(lm.locks, t)
		return false
	}
	_, ok := wp.Included(p, l.path, l.depth)
	return ok
}

func (lm *lockmaster) generateToken() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("urn:uuid:%x-%x-280885-%x",
		r.Int31(), r.Int31(), time.Now().UnixNano())
}

func (lm *lockmaster) unlock(t string) {
	lm.m.Lock()
	defer lm.m.Unlock()
	delete(lm.locks, t)
}

func (lm *lockmaster) refreshLock(tok string, path Path, duration time.Duration) (*lock, error) {
	lm.m.Lock()
	defer lm.m.Unlock()

	p := path.String()

	// We enforce all locks to be a minimum of ten seconds.
	if duration < minLockDuration {
		duration = minLockDuration
	}
	if duration > maxLockDuration {
		duration = maxLockDuration
	}

	l, ok := lm.locks[tok]
	if !ok {
		return nil, fmt.Errorf("unknown lock: %s", tok)
	}
	if l.expired() {
		delete(lm.locks, l.token)
		return nil, errors.New("expired lock")
	}
	if _, ok := wp.Included(p, l.path, l.depth); !ok {
		return nil, errors.New("path not within lock")
	}
	l.duration = duration
	l.touch()
	return l, nil
}

func (lm *lockmaster) createLock(owner string, path Path, depth int, duration time.Duration) (*lock, error) {
	lm.m.Lock()
	defer lm.m.Unlock()

	p := path.String()

	// We enforce all locks to be a minimum of ten seconds.
	if duration < minLockDuration {
		duration = minLockDuration
	}
	if duration > maxLockDuration {
		duration = maxLockDuration
	}

	for _, l := range lm.locks {
		if l.expired() {
			delete(lm.locks, l.token)
			continue
		}

		// Check if the lock covers this path already.
		if _, ok := wp.Included(p, l.path, l.depth); ok {
			return nil, ErrorLocked
		}

		// Check if this crosses another lock.
		if _, ok := wp.Included(l.path, p, depth); ok {
			return nil, ErrorLocked
		}
	}

	l := &lock{
		token:    lm.generateToken(),
		depth:    depth,
		owner:    owner,
		duration: duration,
		modified: time.Now(),
		path:     p,
	}
	lm.locks[l.token] = l
	return l, nil
}
