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
	"io"
	"unicode"
)

// Special tokens the lexer can return.
const (
	EOF = -(iota + 1)
	Not
)

type lex struct {
	input []rune
	pos   int
	last  rune // last token peek'ed
}

func newLex(s string) *lex {
	return &lex{
		input: []rune(s),
		pos:   -1,
	}
}

func (l *lex) p(num int) rune {
	np := l.pos + num
	if np < 0 || np >= len(l.input) {
		return EOF
	}
	return l.input[np]
}

func (l *lex) skipWhitespace() {
	for unicode.IsSpace(l.p(1)) {
		l.pos++
	}
}

func (l *lex) peek() rune {
	l.skipWhitespace()
	p := l.p(1)
	if p == 'N' && l.p(2) == 'o' && l.p(3) == 't' {
		p = Not
	}
	l.last = p
	return p
}

func (l *lex) consume() {
	if l.last == Not {
		l.pos += 3
	} else if l.last != EOF {
		l.pos++
	}
}

func (l *lex) tokenText(r rune) string {
	if r == Not {
		return "Not"
	}
	return string(r)
}

func (l *lex) consumeIf(acc func(rune) bool) (string, error) {
	res := ""
	for {
		v := l.p(1)
		if v == EOF {
			return res, io.EOF
		}
		if !acc(v) {
			return res, nil
		}
		l.consume()
		res += l.tokenText(v)
	}
}

func (l *lex) consumeUntil(stop rune) (string, error) {
	s, err := l.consumeIf(func(r rune) bool {
		return r != stop
	})
	if err != nil {
		return s, err
	}
	// Eat the target token.
	l.consume()
	return s, err
}
