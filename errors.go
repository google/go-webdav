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
	"fmt"
	"net/http"
)

// http://www.webdav.org/specs/rfc4918.html#status.code.extensions.to.http11
const (
	StatusMulti               = 207
	StatusUnprocessableEntity = 422
	StatusLocked              = 423
	StatusFailedDependency    = 424
	StatusInsufficientStorage = 507
)

var extStatusText = map[int]string{
	StatusMulti:               "Multi-Status",
	StatusUnprocessableEntity: "Unprocessable Entity",
	StatusLocked:              "Locked",
	StatusFailedDependency:    "Failed Dependency",
	StatusInsufficientStorage: "Insufficient Storage",
}

// Error is the common error type used for webdav methods.
type Error struct {
	code  int
	text  string
	cause error
}

// Error codes that are reportable from the API.
var (
	// ErrorNotYetImplemented is intended for use for code in progress.
	ErrorNotYetImplemented = Error{code: http.StatusTeapot, text: "TODO"}
	ErrorBadPath           = Error{code: http.StatusBadRequest, text: "BadPath"}
	ErrorNotFound          = Error{code: http.StatusNotFound, text: "NotFound"}
	ErrorConflict          = Error{code: http.StatusConflict, text: "Conflict"}
	ErrorNotAllowed        = Error{code: http.StatusMethodNotAllowed, text: "NotAllowed"}
	ErrorUnsupportedType   = Error{code: http.StatusUnsupportedMediaType, text: "UnsupportedType"}
	ErrorIsDir             = Error{code: http.StatusMethodNotAllowed, text: "IsDir"}
	ErrorIsNotDir          = Error{code: http.StatusMethodNotAllowed, text: "IsNotDir"}
	ErrorMissingParent     = Error{code: http.StatusConflict, text: "MissingParent"}
	ErrorUnderrun          = Error{code: http.StatusBadRequest, text: "Underrun"}
	ErrorBadHost           = Error{code: http.StatusBadGateway, text: "BadHost"}
	ErrorBadDepth          = Error{code: http.StatusBadRequest, text: "BadDepth"}
	ErrorBadDest           = Error{code: http.StatusBadRequest, text: "BadDest"}
	ErrorBadPropfind       = Error{code: http.StatusBadRequest, text: "BadPropfind"}
	ErrorDestExists        = Error{code: http.StatusPreconditionFailed, text: "DestExists"}
	ErrorSameFile          = Error{code: http.StatusForbidden, text: "SameFile"}
	ErrorBadProppatch      = Error{code: http.StatusBadRequest, text: "BadProppatch"}
	ErrorLocked            = Error{code: StatusLocked, text: "Locked"}
	ErrorBadLock           = Error{code: http.StatusBadRequest, text: "BadLock"}
)

// WithCause is used to chain a cause onto a reported HTTP error code.
func (e Error) WithCause(cause error) Error {
	return Error{code: e.code, text: e.text, cause: cause}
}

// HTTPCode gets the HTTP error code appropriate for the error.
func (e Error) HTTPCode() int {
	return e.code
}

// HTTPStatus gets the HTTP status text to use for the error.
func (e Error) HTTPStatus() string {
	if t, ok := extStatusText[e.code]; ok {
		return t
	}
	return http.StatusText(e.code)
}

// InternalCause gets the underlying cause of the error, should not generally
// be provided to the client.
func (e Error) InternalCause() error {
	return e.cause
}

func (e Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%d %s : %s (%s)", e.code, e.HTTPStatus(), e.text, e.cause)
	}
	return fmt.Sprintf("%d %s : %s", e.code, e.HTTPStatus(), e.text)
}

func (e Error) String() string {
	return e.Error()
}
