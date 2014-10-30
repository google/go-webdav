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

package cond

import (
	"log"
	"testing"
)

func TestParse(t *testing.T) {
	examples := map[string]bool{
		"foobar":                false,
		"(a":                    false,
		"([b":                   false,
		"(Not a":                false,
		"":                      true,
		"(a)":                   true,
		"(a) (b)":               true,
		"(Not a Not b Not [d])": true,
		"(Not a) (Not b)":       true,
		"([a])":                 true,
	}

	for s, exp := range examples {
		log.Println("parsing:", s)
		o, err := ParseIfTag(s)
		ok := err == nil
		if exp != ok {
			t.Errorf("'%s' did not parse as expected, got [%+v]: %v", s, o, err)
		} else {
			log.Printf("got [%+v]: %v", o, err)
		}
	}
}
