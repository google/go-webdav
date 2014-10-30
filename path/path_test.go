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
	"testing"
)

func TestPathInTree(t *testing.T) {
	if !PathInTree("/", "/") {
		t.Error("/ should contain /")
	}
	if !PathInTree("/foo", "/") {
		t.Error("/ should contain /foo")
	}
	if !PathInTree("/foo/bar", "/") {
		t.Error("/ should contain /foo/bar")
	}
	if PathInTree("/foo/zoo", "/foo/bar") {
		t.Error("/foo/bar should not contain /foo/zoo")
	}
	if PathInTree("/foozy", "/doozy") {
		t.Error("/doozy should not contain /foozy")
	}
}

func TestIncluded(t *testing.T) {
	if _, ok := Included("/", "/", 0); !ok {
		t.Error("/ should include / with depth 0")
	}
	if _, ok := Included("/foo", "/", 0); ok {
		t.Error("/ should not include /foo with depth 0")
	}
	if _, ok := Included("/foo", "/", 1); !ok {
		t.Error("/ should include /foo with depth 1")
	}
	if _, ok := Included("/foo/bar", "/", 1); ok {
		t.Error("/ should not include /foo/bar with depth 1")
	}
}
