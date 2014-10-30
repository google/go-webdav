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

// Package cond is a parser for the If header expected of webdav, to produce
// condition objects to evaluate the header.
package cond

import (
	"fmt"
	"net/url"
	"strings"
)

// Env is the environment for evaluating conditions.
type Env interface {
	// ETag looks up the current ETag for a resource by URI.
	ETag(r string) string
	// Locked tests whether the lock identified by the given token
	// locks the given resource. Note that due to shared locks many
	// may exist.
	Locked(r, l string) bool
}

// Condition is a single condition
type Condition struct {
	Not   bool
	State string
	ETag  string
}

func parseCondition(l *lex) (Condition, error) {
	res := Condition{}
	tok := l.peek()
	if tok == Not {
		res.Not = true
		l.consume()
		tok = l.peek()
	}
	if tok == '[' {
		l.consume()
		et, err := l.consumeUntil(']')
		res.ETag = et
		if et == "" {
			return res, fmt.Errorf("empty etag")
		}
		return res, err
	}
	tt, err := l.consumeIf(func(r rune) bool {
		return r != ')' && r != ' '
	})
	if len(tt) >= 2 && tt[0] == '<' {
		tt = tt[1 : len(tt)-1]
	}
	res.State = tt
	if tt == "" {
		return res, fmt.Errorf("empty condition")
	}
	return res, err
}

// Eval determines the conditions state in the given environment
// for the given resource.
func (c *Condition) Eval(e Env, r string) bool {
	res := false
	if c.State != "" {
		res = e.Locked(r, c.State)
	} else {
		res = e.ETag(r) == c.ETag
	}
	if c.Not {
		res = !res
	}
	return res
}

func (c *Condition) String() string {
	prefix := ""
	if c.Not {
		prefix = "Not "
	}
	if c.State != "" {
		return prefix + c.State
	}
	return prefix + "[" + c.ETag + "]"
}

// ConditionList represents a set of conditions that are AND'ed together.
type ConditionList struct {
	Resource   string
	Conditions []Condition
}

func parseList(l *lex) (*ConditionList, error) {
	res := &ConditionList{}
	tok := l.peek()
	if tok == '<' {
		l.consume()
		rt, err := l.consumeUntil('>')
		res.Resource = rt
		if err != nil || rt == "" {
			return res, fmt.Errorf("could not parse resource: %v", err)
		}
		tok = l.peek()
	}
	if tok != '(' {
		return res, fmt.Errorf("expected ( got %v", tok)
	}
	l.consume()
	tok = l.peek()
	for tok != ')' && tok != EOF {
		c, err := parseCondition(l)
		res.Conditions = append(res.Conditions, c)
		if err != nil {
			return res, fmt.Errorf("could not parse condition: %v", err)
		}
		tok = l.peek()
	}
	if tok != ')' {
		return res, fmt.Errorf("expected ) got %v", tok)
	}
	l.consume()
	return res, nil
}

// Eval determines the lists state in the given environment, also providing
// a default resource URI in case this list lacks one.
func (l *ConditionList) Eval(e Env, rdef string) bool {
	if l.Resource != "" {
		rdef = l.Resource
	}
	for _, c := range l.Conditions {
		if !c.Eval(e, rdef) {
			return false
		}
	}
	return true
}

func (l *ConditionList) String() string {
	prefix := ""
	if l.Resource != "" {
		prefix += "<" + l.Resource + "> "
	}
	str := make([]string, len(l.Conditions))
	for i, c := range l.Conditions {
		str[i] = c.String()
	}
	return prefix + "(" + strings.Join(str, " ") + ")"
}

// IfTag represents a complete If header, lists are evaluated by OR'ing them
// together. Thus the header forms a DNF condition.
type IfTag struct {
	Lists []*ConditionList
}

// Eval determines the header's state in the given environment.
func (t *IfTag) Eval(e Env, rdef string) bool {
	for _, l := range t.Lists {
		if l.Eval(e, rdef) {
			return true
		}
	}
	return false
}

// GetAllTokens gets all lock tokens from the given IfTag.
func (t *IfTag) GetAllTokens() []string {
	var res []string
	for _, l := range t.Lists {
		for _, c := range l.Conditions {
			if c.State != "" {
				res = append(res, c.State)
			}
		}
	}
	return res
}

// GetSingleState gets the singular token state from this If header, it will
// report whether one could be successfully extracted (note, the presence of
// more than one, being ambiguous, counts as failure).
func (t *IfTag) GetSingleState() (string, bool) {
	if len(t.Lists) != 1 {
		return "", false
	}
	l := t.Lists[0]
	if len(l.Conditions) != 1 {
		return "", false
	}
	c := l.Conditions[0]
	if c.ETag != "" {
		return "", false
	}
	if c.Not {
		return "", false
	}
	return c.State, true
}

// RewriteHosts rewrites all resource URIs to be relative to the given host,
// checking that they match at the same time.
func (t *IfTag) RewriteHosts(h string) error {
	for _, l := range t.Lists {
		if l.Resource == "" {
			continue
		}

		url, err := url.Parse(l.Resource)
		if err != nil {
			return err
		}
		if url.Host != "" && url.Host != h {
			return fmt.Errorf("Bad host")
		}
		l.Resource = url.Path
	}
	return nil
}

func (t *IfTag) String() string {
	str := make([]string, len(t.Lists))
	for i, l := range t.Lists {
		str[i] = l.String()
	}
	return strings.Join(str, " ")
}

// ParseIfTag parses the If HTTP header.
func ParseIfTag(s string) (*IfTag, error) {
	res := &IfTag{}
	l := newLex(s)
	for {
		tok := l.peek()
		if tok == EOF {
			break
		}
		list, err := parseList(l)
		res.Lists = append(res.Lists, list)
		if err != nil {
			return res, fmt.Errorf("could not parse list: %v", err)
		}
	}
	return res, nil
}
