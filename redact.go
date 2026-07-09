// Copyright 2020 Matthew Holt
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

package xcaddy

import (
	"bytes"
	"regexp"
	"sync"
)

// redactedPlaceholder replaces sensitive values in step output.
const redactedPlaceholder = "[REDACTED]"

// maxRedactBuffer caps how much of an unterminated line the
// redactor will hold before force-flushing it (masked) anyway.
const maxRedactBuffer = 1 << 20

// redactRule masks either an exact literal value or a pattern.
type redactRule struct {
	literal []byte         // if non-nil, exact-match redaction
	re      *regexp.Regexp // otherwise, pattern redaction
	repl    []byte         // replacement for pattern matches
}

func (r redactRule) apply(line []byte) []byte {
	if r.literal != nil {
		return bytes.ReplaceAll(line, r.literal, []byte(redactedPlaceholder))
	}
	return r.re.ReplaceAll(line, r.repl)
}

// builtinCredentialRules mask common credential shapes. They are a
// best-effort backstop for publishable logs, not a guarantee.
var builtinCredentialRules = []redactRule{
	// userinfo in URLs: scheme://user:password@host
	{re: regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@]+@`), repl: []byte("${1}" + redactedPlaceholder + "@")},
	// GitHub tokens (classic and fine-grained)
	{re: regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`), repl: []byte(redactedPlaceholder)},
	{re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`), repl: []byte(redactedPlaceholder)},
	// GitLab personal access tokens
	{re: regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{16,}`), repl: []byte(redactedPlaceholder)},
	// Slack tokens
	{re: regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]{10,}`), repl: []byte(redactedPlaceholder)},
	// AWS access key IDs
	{re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), repl: []byte(redactedPlaceholder)},
}

// key block delimiters for PEM-encoded private key material
var (
	keyBlockBegin = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY( BLOCK)?-----`)
	keyBlockEnd   = regexp.MustCompile(`-----END [A-Z0-9 ]*PRIVATE KEY( BLOCK)?-----`)
)

// redactor rewrites sensitive values out of a byte stream before it
// reaches a step's buffered pipe. It is line-buffered: bytes are
// held until a newline arrives and each complete line is masked as
// a unit, so a secret can never slip through by straddling a write
// boundary. Closing the redactor flushes any unterminated final
// line (masked) and closes the underlying pipe.
type redactor struct {
	dst           *bufferedPipe
	rules         []redactRule
	maskKeyBlocks bool

	mu         sync.Mutex
	buf        []byte
	inKeyBlock bool
}

func (r *redactor) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			break
		}
		if _, err := r.dst.Write(r.mask(r.buf[:i+1])); err != nil {
			return len(p), err
		}
		r.buf = r.buf[i+1:]
	}
	// don't hold a pathological unterminated line forever
	if len(r.buf) > maxRedactBuffer {
		if _, err := r.dst.Write(r.mask(r.buf)); err != nil {
			return len(p), err
		}
		r.buf = nil
	}
	return len(p), nil
}

// Close flushes any buffered partial line (masked) and closes the
// underlying pipe, delivering EOF to the step's reader.
func (r *redactor) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) > 0 {
		_, _ = r.dst.Write(r.mask(r.buf))
		r.buf = nil
	}
	return r.dst.Close()
}

func (r *redactor) mask(line []byte) []byte {
	if r.maskKeyBlocks {
		// mask entire private key blocks, line by line
		if r.inKeyBlock {
			if keyBlockEnd.Match(line) {
				r.inKeyBlock = false
			}
			return []byte(redactedPlaceholder + "\n")
		}
		if keyBlockBegin.Match(line) {
			r.inKeyBlock = true
			return []byte(redactedPlaceholder + "\n")
		}
	}
	for _, rule := range r.rules {
		line = rule.apply(line)
	}
	return line
}
