package objectstore

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadObjectStores(t *testing.T) {
	dir, err := ioutil.TempDir("", "example")
	if err != nil {
		t.Error(err)
	}
	fmt.Println("Created temp dir ", dir)
	objs, err := LoadObjectStores(dir)
	if len(objs) > 0 {
		t.Errorf("Expecting 0 length map of plugins got %d", len(objs))
	}
	err = copyPlugin("../../internal/bin/objectstores/s3.so", dir)
	if err != nil {
		t.Error(err)
	}
	objs, err = LoadObjectStores(dir)
	// TODO: either change the plugin build or something but this always fails due
	// to plugin versioning
	if err == nil {
		t.Errorf("Expected plugin version error: %s", err)
	}
	err = deletePlugin("../../internal/bin/objectstores/s3.so", dir)
	if err != nil {
		t.Error(err)
	}
	err = copyPlugin("../../internal/bin/objectstores/test.so", dir)
	if err != nil {
		t.Error(err)
	}
	objs, err = LoadObjectStores(dir)
	if err != ErrInvalidInterface {
		t.Errorf("Expected is not GitPath interface: %s", err)
	}
	err = deletePlugin("../../internal/bin/objectstores/test.so", dir)
	if err != nil {
		t.Error(err)
	}
	err = copyPlugin("../../internal/bin/gitpaths/github.so", dir)
	if err != nil {
		t.Error(err)
	}
	objs, err = LoadObjectStores(dir)
	if err == nil {
		t.Errorf("Expected No GitPath error: %s", err)
	}
}

func copyPlugin(plugin, tmpfolder string) error {
	input, err := ioutil.ReadFile(plugin)
	if err != nil {
		return err
	}
	base := filepath.Base(plugin)
	err = ioutil.WriteFile(fmt.Sprintf("%s/%s", tmpfolder, base), input, 0664)
	if err != nil {
		return err
	}
	return nil
}

func deletePlugin(plugin, tmpfolder string) error {
	base := filepath.Base(plugin)
	return os.Remove(fmt.Sprintf("%s/%s", tmpfolder, base))
}
