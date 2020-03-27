# Plugins

Currently slipway supports two types of plugins:
- ObjectStore
- GitPath

All are loaded in slipway by their type and by the filename.

Gitpath plugins would be found under the "gitpath" folder.  So the Github plugin
is located under ```/path/to/plugins/folder/gitpaths/github.so```

You don't need to worry about any of that if you just write your plugins under the folder

```internal/plugins```

Then run ```make```

This will put the plugins in the correct folder locally.

```internal/bin```

The Dockerfile will also place them in the correct folder for being deployed to a cluster.

## ObjectStore

The ObjectStore plugin will just write yaml files to a storage layer after they have been applied.

### Building a ObjectStore Plugin

Create new folder under ```internal/plugins/objectstores```
```bash
$ mkdir internal/plugins/objectstores/mynewstorage && \
cat <<'EOF' > internal/plugins/objectstores/mynewstorage/mynewstorage.go
package main

import (
	"github.com/slipway-gitops/slipway/pkg/objectstore"
)

var (
	// !!!Required!!!
	// There needs to be a public variable available called ObjectStore
	ObjectStore mynewstorage
)

type mynewstorage struct {
}

// Save the yaml as a byte array.
func (me mynewstorage) Save(hash, operation string, yaml []byte) error {
	// Store your file here
	return nil
}

func (me mynewstorage) New(bucket string) objectstore.ObjectStore {
	// Instantiate and
	// save your bucket here
	return me
}
EOF
```

The you can run make
```bash
$ make
```

You should have a new plugin that does nothing in ```internal/bin/objectstores/mynewstorage.so```

To load this as the storage layer
```yaml
      store:
        type: mynewstorage
```


## GitPath

The GitPath plugin determines how a git server stores refrences to git hashes.  For a better understanding you can run
```bash
git ls-remote
```
If we run this against this repo we would get something like this:
```bash
$ git ls-remote https://github.com/slipway-gitops/slipway.git
1648dc171b91dcbdb22a582fcf5da3cfd49d8285	refs/heads/master
fdf51c7f3866415ca42571fe0bba544b18a6750d	refs/pull/24/head
9ad3ad9b8fdd2e2dc9ae29e28430bb5ec3b962f8	refs/pull/25/head
832d4c461247818193dec8a10bbfefaf2a7de583	refs/pull/25/merge
```
* this was edited for brevity.

Active pull requests will have a reference called refs/pull/#/merge and one called refs/pull/#/head.

Merged pull requests do not have the "merge" reference.

We can compare this to how Gitlab stores their codebase on Gitlab.

```bash
git ls-remote https://gitlab.com/gitlab-org/gitlab.git
3f06674c62632a5f5736662f37e112a86563f64f	refs/merge-requests/20582/head
c5a2ed73647f5599f94628122cf2b7df1e23642e	refs/merge-requests/20582/merge
360c79fb928f161de98fe869d4ec7ae05d73640b        refs/heads/master
```
* this was edited for brevity.

Note the difference in how the references are stored for pull requests.

Ideally we will have a system that will be handle all these differences but until that time
this plugin system allows users to write their own parsers.


### Building a GitPath Plugin

Create new folder under ```internal/plugins/gitpaths```
```bash
$ mkdir internal/plugins/gitpaths/mynewgitpath && \
cat <<'EOF' > internal/plugins/gitpaths/mynewgitpath/mynewgitpath.go
package main
import (
	"github.com/slipway-gitops/slipway/pkg/gitpath"
)

var (
	// !!!Required!!!
	// There needs to be a public variable available called GitPath
	GitPath mygitpath
)

type mygitpath struct {
}

// New Just gives you a chance to instantiate the reference parser.  It returns itself.
func (me mygitpath) New(optype string, regex string, reference string) (gitpath.GitPath, error) {
	return me, nil
}

// This will return true if based on the regex and the type it matched the refrence.
func (me mygitpath) Match() bool {
	return true
}

// This returns the title so "refs/heads/master" would return "master"
// "refs/pull/24/head" would be "pull-24"
// "refs/tags/v0.10.0" would be "v0.10.0"
func (g mygitpath) Title() string {
	return "thisisatitle"
}

EOF
```

The you can run make
```bash
$ make
```

You should have a new plugin that does nothing in ```internal/bin/gitpaths/mynewgitpath.so```

To load this as the git path parser, just specify it on the GitRepo.
```yaml
      gitpath: mynewgitpath
```


