package controllers

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	v1 "github.com/slipway-gitops/slipway/api/v1"
	apps "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetHashNames() error {
	err := hashtestlogger.ReadUntilLog("name", "controllers")
	if err != nil {
		return err
	}
	err = hashtestlogger.ReadUntilLog("name", "Hash")
	if err != nil {
		return err
	}
	return nil
}

func GetHashValues(vals ...string) error {
	var retvals []string
	for _ = range vals {
		val, err := hashtestlogger.ReadUntilType("value")
		if err != nil {
			return err
		}
		retvals = append(retvals, val)
	}
	if !reflect.DeepEqual(retvals, vals) {
		return fmt.Errorf("Expected values %v", vals)
	}
	return nil

}

func testHashReconcile(t *testing.T) {

	hash := &v1.Hash{
		Spec: v1.HashSpec{
			GitRepo: "notvalid",
			Operations: []v1.Operation{
				v1.Operation{
					Name:         "test",
					Transformers: []v1.Transformer{},
					Type:         "tag",
				},
			},
		},
	}
	obj := NewHashHandler(hash)
	err := obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = GetHashValues("hash", fmt.Sprintf("/%s", hash.ObjectMeta.Name))
	if err != nil {
		t.Error(err)
	}
	err = hashtestlogger.ReadUntilRegex("error",
		"^unable to fetch Owner Repo: GitRepo.git.gitops.slipway.org*")
	if err != nil {
		t.Error(err)
	}
	repo := &v1.GitRepo{
		Spec: v1.GitRepoSpec{
			Uri: "thisisbasicinvalid",
			Operations: []v1.Operation{
				v1.Operation{
					Name:         "test",
					Transformers: []v1.Transformer{},
					Type:         "tag",
				},
			},
		},
	}
	gitobj := NewGitHandler(repo)
	err = gitobj.Create()
	if err != nil {
		t.Error(err)
	}
	hash.Spec.GitRepo = gitobj.Name()
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = hashtestlogger.ReadUntilLog("info",
		"No Storage type selected: []")
	if err != nil {
		t.Error(err)
	}
	hash.Spec.Store = &v1.Store{Type: "invalid"}
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = hashtestlogger.ReadUntilRegex("error",
		"^No plugin for this objectstore type*")
	if err != nil {
		t.Error(err)
	}
	hash.Spec.Store = &v1.Store{Type: "s3"}
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = hashtestlogger.ReadUntilRegex("error",
		"^Invalid path*")
	if err != nil {
		t.Error(err)
	}
	hash.Spec.Operations[0].Path = "thisisapath"
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = hashtestlogger.ReadUntilRegex("error",
		"^Unable to fetch kustomize manifests*")
	if err != nil {
		t.Error(err)
	}
	hash.Spec.Operations[0].Path = "git@github.com:slipway-gitops/slipway-example-app.git"
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = hashtestlogger.ReadUntilRegex("error",
		"^Unable to fetch kustomize manifests: unable to find one of*")
	if err != nil {
		t.Error(err)
	}
	hash.Spec.Operations[0].Path = "git@github.com:slipway-gitops/slipway-example-app.git//kustomize/base"
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = hashtestlogger.ReadUntilRegex("info",
		"^Successfully pulled*")
	if err != nil {
		t.Error(err)
	}
	for i := 1; i <= 3; i++ {
		err = hashtestlogger.ReadUntilRegex("info",
			"^Operation result*")
		if err != nil {
			t.Error(err)
		}
	}
	dep := &apps.Deployment{}
	ctx := context.TODO()
	err = k8sClient.Get(ctx, client.ObjectKey{Name: "the-deployment", Namespace: "default"}, dep)
	if err != nil {
		t.Error(err)
	}
	if dep.ObjectMeta.OwnerReferences[0].Name != obj.Name() {
		t.Error("Expected Hash to own deployment")
	}
}
