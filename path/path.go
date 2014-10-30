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

package path

import (
	"net/url"
	gp "path"
	"strings"
)

// InTree determines if a given path is within a subtree.
func InTree(path, subtree string) bool {
	if path == subtree {
		return true
	}
	if !strings.HasSuffix(subtree, "/") {
		subtree += "/"
	}
	return strings.HasPrefix(path, subtree)
}

// Included determines if a given name is included in a subtree, subject to the
// provided depth restriction. If it is included, it returns the name relative
// to that subtree's name.
func Included(fn, subtree string, depth int) (string, bool) {
	if fn == subtree {
		return "", true
	}
	if !InTree(fn, subtree) {
		return "", false
	}
	fn = gp.Clean(fn[len(subtree):])
	fd := len(strings.Split(fn, "/"))
	if depth >= 0 && fd > depth {
		return "", false
	}
	return fn, true
}

// URLEncode encodes a string so it is safe to place in a URL.
func URLEncode(s string) string {
	u := url.URL{Path: s}
	return u.RequestURI()
}
