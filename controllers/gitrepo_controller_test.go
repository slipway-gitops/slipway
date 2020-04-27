package controllers

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"

	v1 "github.com/slipway-gitops/slipway/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestMain(m *testing.M) {
	ctx := context.TODO()
	err := SetupTestGitRepo(ctx)
	if err != nil {
		fmt.Println(err)
	}
	ex := m.Run()
	err = TearDownTestGitRepo()
	if err != nil {
		fmt.Println(err)
	}
	os.Exit(ex)

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
		return fmt.Errorf("Expected values %v", vals)
	}
	return nil

}

func TestGitRepoReconcile(t *testing.T) {
	err := GetNames()
	if err != nil {
		t.Error(err)
	}
	ctx := context.TODO()
	myKind := &v1.GitRepo{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testresource",
		},
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
	err = k8sClient.Create(ctx, myKind)
	if err != nil {
		t.Error(err)
	}
	err = GetValues("gitrepo", "/testresource")
	if err != nil {
		t.Error(err)
	}
	err = gittestlogger.ReadUntilLog("error", "remote access error: repository not found -- []")
	if err != nil {
		t.Error(err)
	}
	myKindGet := &v1.GitRepo{}
	err = k8sClient.Get(ctx, client.ObjectKey{
		Name: "testresource",
	}, myKindGet)
	if err != nil {
		t.Error(err)
	}
	myKind.Spec.Uri = "git@github.com:slipway-gitops/slipway-example-app.git"
	myKind.Spec.GitPath = "invalid"

	err = k8sClient.Update(ctx, myKind)
	if err != nil {
		t.Error(err)
	}
	err = GetValues("gitrepo", "/testresource")
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
	err = k8sClient.Update(ctx, myKind)
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
	myKind = &v1.GitRepo{}
	err = k8sClient.Get(ctx, client.ObjectKey{
		Name: "testresource",
	}, myKind)
	if err != nil {
		t.Error(err)
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
	err = k8sClient.Update(ctx, myKind)
	if err != nil {
		t.Error(err)
	}
	err = GetValues("gitrepo", "/testresource")
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
	if hashListGet.Items[0].Spec.Operations[0].ReferenceTitle != "v1.1.0" {
		t.Error("Expectected highest version v1.1.0")
	}
	//t.Error(litter.Sdump(hashListGet))
}
