package controllers

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"

	v1 "github.com/slipway-gitops/slipway/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMain(m *testing.M) {
	err := SetupTestEnv()
	if err != nil {
		fmt.Println(err)
	}
	ex := m.Run()
	err = TearDownTestEnv()
	if err != nil {
		fmt.Println(err)
	}
	os.Exit(ex)

}

func TestAll(t *testing.T) {
	err := SetupTestGitRepo()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("TestGitRepoReconcile", testGitRepoReconcile)
	TearDownTestGitRepo()
	err = SetupTestHash()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("TestHashReconcile", testHashReconcile)
	TearDownTestHash()
}

func GetNames() error {
	err := gittestlogger.ReadUntilLog("name", "controllers")
	if err != nil {
		return err
	}
	err = gittestlogger.ReadUntilLog("name", "GitRepo")
	if err != nil {
		return err
	}
	return nil
}

func GetValues(vals ...string) error {
	var retvals []string
	for _ = range vals {
		val, err := gittestlogger.ReadUntilType("value")
		if err != nil {
			return err
		}
		retvals = append(retvals, val)
	}
	if !reflect.DeepEqual(retvals, vals) {
		return fmt.Errorf("Expected values %v got %v", vals, retvals)
	}
	return nil

}

func testGitRepoReconcile(t *testing.T) {
	ctx := context.TODO()
	err := GetNames()
	if err != nil {
		t.Error(err)
	}
	myKind := &v1.GitRepo{
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
	obj := NewGitHandler(myKind)
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = GetValues("gitrepo", obj.NamespacedName())
	if err != nil {
		t.Error(err)
	}
	err = gittestlogger.ReadUntilLog("error", "remote access error: repository not found -- []")
	if err != nil {
		t.Error(err)
	}
	err = obj.Get()
	if err != nil {
		t.Error(err)
	}
	myKind.Spec.Uri = "git@github.com:slipway-gitops/slipway-example-app.git"
	myKind.Spec.GitPath = "invalid"
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = gittestlogger.ReadUntilLog("error", "No plugin for this gitpath type: <nil> -- []")
	if err != nil {
		t.Error(err)
	}
	myKind.Spec.GitPath = "github"
	myKind.Spec.Operations = []v1.Operation{
		v1.Operation{
			Name:         "test",
			Path:         "git@github.com:slipway-gitops/slipway-example-app.git//kustomize/base",
			Type:         v1.OpType("branch"),
			Reference:    "master",
			Transformers: []v1.Transformer{},
		},
	}
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = gittestlogger.ReadUntilRegex("info", `^Operation result: \[created Hash for GitRepo*`)
	if err != nil {
		t.Error(err)
	}
	hashListGet := &v1.HashList{}
	err = k8sClient.List(ctx, hashListGet)
	if err != nil {
		t.Error(err)
	}
	if len(hashListGet.Items) != 1 {
		t.Fatal("Expected one hash item returned")
	}
	or := hashListGet.Items[0].ObjectMeta.OwnerReferences
	hashCreate := &v1.Hash{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test",
			OwnerReferences: or,
		},
		Spec: v1.HashSpec{
			Operations: []v1.Operation{},
		},
	}
	err = k8sClient.Create(ctx, hashCreate)
	if err != nil {
		t.Error(err)
	}
	err = gittestlogger.ReadUntilRegex("info", "^deleted old hash*")
	if err != nil {
		t.Error(err)
	}
	hashListGet = &v1.HashList{}
	err = k8sClient.List(ctx, hashListGet)
	if err != nil {
		t.Error(err)
	}
	if len(hashListGet.Items) != 1 {
		t.Error("Expected one hash item returned")
	}
	myKind.Spec.Operations = []v1.Operation{
		v1.Operation{
			Name:         "highesttest",
			Path:         "git@github.com:slipway-gitops/slipway-example-app.git//kustomize/base",
			Transformers: []v1.Transformer{},
			Type:         v1.OpType("highesttag"),
			Reference:    "v1.1.[0-9]",
		},
	}
	err = obj.Create()
	if err != nil {
		t.Error(err)
	}
	err = gittestlogger.ReadUntilRegex("info", `^Operation result: \[created Hash for GitRepo*`)
	if err != nil {
		t.Error(err)
	}
	hashListGet = &v1.HashList{}
	err = k8sClient.List(ctx, hashListGet)
	if err != nil {
		t.Error(err)
	}
	found := false
	for _, h := range hashListGet.Items {
		if h.Spec.Operations[0].ReferenceTitle == "v1.1.0" {
			found = true
		}
	}
	if !found {
		t.Error("Expectected highest version v1.1.0")
	}
	err = obj.Clean()
	if err != nil {
		t.Error(err)
	}
	hashListGet = &v1.HashList{}
	err = k8sClient.List(ctx, hashListGet)
	if err != nil {
		t.Error(err)
	}
	for _, h := range hashListGet.Items {
		err = k8sClient.Delete(ctx, &h)
		if err != nil {
			t.Error(err)
		}
	}
}
