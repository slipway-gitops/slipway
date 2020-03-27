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
/*
ObjectStore defines an interface that can be used as a way to store deployed manifests.
*/

package objectstore

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
	ErrInvalidType      = errors.New("Invalid ObjectStore Type")
	ErrInvalidInterface = errors.New("Invalid Plugin does not implement ObjectStore")
)

// ObjectStore is the interface a objectstore plugin needs to implement
type ObjectStore interface {
	// Saves manifests
	Save(hash, operation string, yaml []byte) error
	New(bucket string) ObjectStore
}

// LoadObjectStores loads all the available plugins from the path
func LoadObjectStores(path string) (map[string]ObjectStore, error) {
	ostores := make(map[string]ObjectStore)
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return ostores, err
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), fileextension) {
			plug, err := plugin.Open(fmt.Sprintf("%v/%v", path, f.Name()))
			if err != nil {
				return ostores, err
			}
			symostore, err := plug.Lookup("ObjectStore")
			if err != nil {
				return ostores, err
			}
			var ostore ObjectStore
			ostore, ok := symostore.(ObjectStore)
			if !ok {
				return ostores, ErrInvalidInterface
			}
			ostores[strings.TrimSuffix(f.Name(), fileextension)] = ostore

		}
	}
	return ostores, nil
}
