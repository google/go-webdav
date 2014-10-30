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

package xml

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"

	wp "github.com/google/go-webdav/path"
)

var blankName xml.Name

func x2s(xn xml.Name) string {
	return xn.Space + ":" + xn.Local
}

func s2x(s string) xml.Name {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return xml.Name{Local: s}
	}
	return xml.Name{
		Space: s[:idx],
		Local: s[idx+1:],
	}
}

type Any struct {
	XMLName xml.Name
	XMLNS   string `xml:"xmlns,attr"`
	Value   string `xml:",chardata"`
	Inner   string `xml:",innerxml"`
}

func NewAny(n string) Any {
	xn := s2x(n)
	a := Any{XMLName: xn, XMLNS: xn.Space}
	// Eliminate the space, we manually set it as Go doesn't have
	// great support for nested namespace definitions.
	// TODO(nmvc): Stop doing this.
	a.XMLName.Space = ""
	return a
}

type prop struct {
	XMLName xml.Name `xml:"prop"`
	XMLNS   string   `xml:"xmlns,attr,omitempty"`
	Any     []Any    `xml:",any"`
}

type multiProp struct {
	XMLName    xml.Name `xml:"propstat"`
	Prop       prop     `xml:"prop,omitempty"`
	PropStatus string   `xml:"status,omitempty"`
}

type multiResponse struct {
	XMLName xml.Name `xml:"response"`
	Href    string   `xml:"href"`
	Status  string   `xml:"status,omitempty"`
	Props   []multiProp
}

// MultiStatus is used to construct a response for multiple URIs
type MultiStatus struct {
	XMLName  xml.Name `xml:"multistatus"`
	XMLNS    string   `xml:"xmlns,attr"`
	Response []multiResponse
}

func NewMultiStatus() *MultiStatus {
	return &MultiStatus{
		XMLNS: "DAV:",
	}
}

func (m *MultiStatus) AddPropStatus(href string, found, missing []Any) {
	r := multiResponse{Href: wp.UrlEncode(href)}
	if len(found) > 0 {
		r.Props = append(r.Props, multiProp{
			Prop:       prop{Any: found},
			PropStatus: "HTTP/1.1 200 OK",
		})
	}
	if len(missing) > 0 {
		r.Props = append(r.Props, multiProp{
			Prop:       prop{Any: missing},
			PropStatus: "HTTP/1.1 404 Not Found",
		})
	}
	m.Response = append(m.Response, r)
}

func (m *MultiStatus) AddStatus(href string, err error) {
	m.Response = append(m.Response, multiResponse{
		Href:   wp.UrlEncode(href),
		Status: err.Error(),
	})
}

// http://www.webdav.org/specs/rfc4918.html#status.code.extensions.to.http11
const (
	StatusMulti = 207
)

func (m *MultiStatus) Send(w http.ResponseWriter) {
	b, err := xml.MarshalIndent(m, "", " ")
	if err != nil {
		panic(err)
	}
	b = append([]byte(xml.Header), b...)
	w.WriteHeader(StatusMulti)
	w.Header().Set("Content-Length", string(len(b)))
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write(b)
}

type propfind struct {
	XMLName  xml.Name  `xml:"propfind"`
	AllProp  *struct{} `xml:"allprop"`
	PropName *struct{} `xml:"propname"`
	Prop     prop
}

type PropFindRequest struct {
	AllProp, PropName bool
	PropertyNames     []string
}

// ParsePropFind parses a PROPFIND request to produce the property
// data requested.
func ParsePropFind(in io.Reader) (PropFindRequest, error) {
	req := PropFindRequest{}

	d := xml.NewDecoder(in)
	pf := propfind{}
	err := d.Decode(&pf)
	if err != nil {
		return req, err
	}

	req.AllProp = pf.AllProp != nil
	req.PropName = pf.PropName != nil

	names := make([]string, 0, len(pf.Prop.Any))
	for _, v := range pf.Prop.Any {
		if v.XMLName.Local == "" {
			continue
		}
		names = append(names, x2s(v.XMLName))
	}
	req.PropertyNames = names
	return req, nil
}

type PropPatchRequest struct {
	Set, Remove map[string]string
}

// ParsePropPatch parses a PROPPATCH request to produce the updates
// that have been requested.
func ParsePropPatch(in io.Reader) (PropPatchRequest, error) {
	// We must manually use the XML decoder to respect the order
	// of fields in the request.
	dec := xml.NewDecoder(in)

	req := PropPatchRequest{
		Set:    make(map[string]string),
		Remove: make(map[string]string),
	}

	// Find the update block.
	_, err := findToken(dec, "propertyupdate", "")
	if err != nil {
		return req, err
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return req, err
		}

		if ee, ok := tok.(xml.EndElement); ok {
			if ee.Name.Local == "propertyupdate" {
				break
			}
			continue
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		// We only care about set or remove internal elements.
		if se.Name.Local != "set" && se.Name.Local != "remove" {
			dec.Skip()
			continue
		}

		pt, err := findToken(dec, "prop", se.Name.Local)
		if err != nil {
			return req, err
		}

		p := prop{}
		dec.DecodeElement(&p, pt)

		// Add the value to one map, and remove from the other, this
		// handles the case of conflicting patch directions.
		var add, sub map[string]string
		if se.Name.Local == "set" {
			add = req.Set
			sub = req.Remove
		} else {
			add = req.Remove
			sub = req.Set
		}

		for _, a := range p.Any {
			n := x2s(a.XMLName)
			add[n] = a.Value
			delete(sub, n)
		}
	}
	return req, nil
}

// findToken consumes tokens in the given decoder until either the given
// name is found, EOF, or the given end token is found. In the case of end
// tokens the return is (nil, nil)
func findToken(d *xml.Decoder, name, halt string) (*xml.StartElement, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == name {
				return &se, nil
			}
			d.Skip()
		}
		if ee, ok := tok.(xml.EndElement); ok {
			if ee.Name.Local == halt {
				return nil, nil
			}
		}
	}
}

type lockinfo struct {
	XMLName   xml.Name  `xml:"lockinfo"`
	Exclusive *struct{} `xml:"lockscope>exclusive"`
	Shared    *struct{} `xml:"lockscope>shared"`
	Write     *struct{} `xml:"locktype>write"`
	Owner     string    `xml:"owner",innerxml`
}

type LockRequest struct {
	Owner   string
	Refresh bool
}

// ParseLockRequest parses a LOCK request
func ParseLock(in io.Reader) (LockRequest, error) {
	req := LockRequest{}
	d := xml.NewDecoder(in)
	li := lockinfo{}
	err := d.Decode(&li)
	if err == io.EOF {
		req.Refresh = true
		return req, nil
	} else if err != nil {
		return req, err
	}
	if li.Exclusive == nil {
		return req, errors.New("must be exclusive")
	}
	if li.Shared != nil {
		return req, errors.New("must not be shared")
	}
	if li.Write == nil {
		return req, errors.New("must be write")
	}
	req.Owner = li.Owner
	return req, nil
}

func SendProp(inner Any, w http.ResponseWriter) error {
	p := prop{
		Any:   []Any{inner},
		XMLNS: "DAV:",
	}
	b, err := xml.MarshalIndent(p, "", " ")
	if err != nil {
		return err
	}
	b = append([]byte(xml.Header), b...)
	w.Header().Set("Content-Length", string(len(b)))
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write(b)
	return nil
}
