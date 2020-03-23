/*
GitPath defines an interface that can be used as a refrences parser.
Each Git provider may store references differently specifically in regards
Pull Requests.
*/

package gitpath

import (
	"errors"
	"fmt"
	"io/ioutil"
	"plugin"
	"strings"
)

var (
	fileextension = ".so"
	// General use for Invalid type.
	ErrInvalidType      = errors.New("Invalid Git Operation Type")
	ErrInvalidInterface = errors.New("Invalid Plugin does not implement GitPath")
)

// GitPath is the interface a git path plugin needs to implement
type GitPath interface {
	// returns a copy of itself with the correct settings
	New(optype string, regex string, reference string) (GitPath, error)
	// Returns if the refrences match
	Match() bool
	// returns the reference title like "master".
	Title() string
}

// LoadGitPaths loads all the available plugins from the path
func LoadGitPaths(path string) (map[string]GitPath, error) {
	gitpaths := make(map[string]GitPath)
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return gitpaths, err
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), fileextension) {

			plug, err := plugin.Open(fmt.Sprintf("%v/%v", path, f.Name()))
			if err != nil {
				return gitpaths, err
			}
			symgitpath, err := plug.Lookup("GitPath")
			if err != nil {
				return gitpaths, err
			}

			var gitpath GitPath
			gitpath, ok := symgitpath.(GitPath)
			if !ok {
				return gitpaths, ErrInvalidInterface
			}
			gitpaths[strings.TrimSuffix(f.Name(), fileextension)] = gitpath

		}
	}
	return gitpaths, nil
}
