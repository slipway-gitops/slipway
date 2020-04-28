package controllers

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	v1 "github.com/slipway-gitops/slipway/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	ctx := context.TODO()
	myKind := &v1.Hash{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testhash",
		},
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
	err := k8sClient.Create(ctx, myKind)
	if err != nil {
		t.Error(err)
	}
	err = GetHashValues("hash", "/testhash")
	if err != nil {
		t.Error(err)
	}
	err = hashtestlogger.ReadUntilRegex("error",
		"^unable to fetch Owner Repo: GitRepo.git.gitops.slipway.org*")
	if err != nil {
		t.Error(err)
	}
}
