/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

/// GitPath parser for Reference patterns in github
import (
	"fmt"
	"regexp"
	"strings"

	"github.com/slipway-gitops/slipway/pkg/gitpath"
)

var (
	optypes = map[string]string{
		"pull":       `^refs/pull/[0-9]+/merge$`,
		"branch":     `refs/heads/`,
		"tag":        `refs/tags/`,
		"highesttag": `refs/tags/`,
	}
	GitPath github
)

type github struct {
	optype    string
	regex     *regexp.Regexp
	reference string
}

func (g github) New(optype string, regex string, reference string) (gitpath.GitPath, error) {
	if val, ok := optypes[optype]; !ok {
		return g, gitpath.ErrInvalidType
	} else {
		var err error
		if optype == "pull" {
			g.regex, err = regexp.Compile(val)
			if err != nil {
				return g, err
			}
		} else {
			g.regex, err = regexp.Compile(fmt.Sprintf("^%v%v$", val, regex))
			if err != nil {
				return g, err
			}
		}
	}
	g.reference = reference
	g.optype = optype
	return g, nil
}

func (g github) Match() bool {
	return g.regex.MatchString(g.reference)
}

func (g github) Title() string {
	if g.optype == "pull" {
		return strings.Join(strings.Split(g.reference, "/")[1:3], "-")
	}
	return strings.TrimPrefix(g.reference, optypes[g.optype])
}
