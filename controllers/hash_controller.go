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

package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"

	gitv1 "github.com/slipway-gitops/slipway/api/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstruct "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	ref "k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/kustomize/api/builtins"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

// HashReconciler reconciles a Hash object
type HashReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=hashes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=hashes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=gitrepoes,verbs=get
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=hashes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=*

func (r *HashReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("hash", req.NamespacedName)

	var hash gitv1.Hash
	if err := r.Get(ctx, req.NamespacedName, &hash); err != nil {
		log.Error(err, "unable to fetch Hash")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	var gitrepo gitv1.GitRepo
	if err := r.Get(ctx, types.NamespacedName{Name: hash.Spec.GitRepo}, &gitrepo); err != nil {
		log.Error(err, "unable to fetch Owner Repo")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	fs := filesys.MakeFsOnDisk()
	opts := krusty.MakeDefaultOptions()

	k := krusty.MakeKustomizer(fs, opts)

	sort.Slice(hash.Spec.Operations, func(i, j int) bool {
		return *hash.Spec.Operations[i].Weight < *hash.Spec.Operations[j].Weight
	})
	hash.Status.Objects = nil
	for _, operation := range hash.Spec.Operations {

		m, err := k.Run(operation.Path)
		if err != nil {
			log.Error(err, "unable to fetch kustomize manifests")
			return ctrl.Result{}, err
		}

		for _, t := range operation.Transformers {
			var val string
			switch t.Value {
			case "branch", "pull", "tag":
				val = operation.ReferenceTitle
			case "hash":
				val = hash.Name
			default:
				val = t.Value
			}

			var plugin resmap.TransformerPlugin
			switch t.Type {
			case "annotations":
				plugin = &builtins.AnnotationsTransformerPlugin{
					Annotations: map[string]string{t.Key: val},
					FieldSpecs: []ktypes.FieldSpec{
						ktypes.FieldSpec{
							Path:               "metadata/annotations",
							CreateIfNotPresent: true,
						},
						ktypes.FieldSpec{
							Path:               "spec/template/metadata/annotations",
							CreateIfNotPresent: true,
						},
					},
				}
			case "images":
				plugin = &builtins.ImageTagTransformerPlugin{
					ImageTag: ktypes.Image{
						Name:   t.Key,
						NewTag: val,
					},
					FieldSpecs: []ktypes.FieldSpec{
						ktypes.FieldSpec{
							Path: "spec/containers/image",
						},
						ktypes.FieldSpec{
							Path: "spec/template/spec/containers/image",
						},
					},
				}
			case "labels":
				plugin = &builtins.LabelTransformerPlugin{
					Labels: map[string]string{t.Key: val},
					FieldSpecs: []ktypes.FieldSpec{
						ktypes.FieldSpec{
							Path:               "metadata/labels",
							CreateIfNotPresent: true,
						},
						ktypes.FieldSpec{
							Path:               "spec/template/metadata/labels",
							CreateIfNotPresent: true,
						},
					},
				}
			case "namespace":
				plugin = &builtins.NamespaceTransformerPlugin{
					ObjectMeta: ktypes.ObjectMeta{
						Namespace: val,
					},
					FieldSpecs: []ktypes.FieldSpec{
						ktypes.FieldSpec{
							Path:               "metadata/namespace",
							CreateIfNotPresent: true,
						},
					},
				}
				ns := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: val},
				}
				if err := ctrl.SetControllerReference(
					&hash,
					&ns,
					r.Scheme); err != nil {
					log.Error(err,
						"unable to create namespace for hash",
						"hash",
						hash,
						"namespace",
						ns)
					return ctrl.Result{}, err
				}
				if objRef, err := ref.GetReference(
					r.Scheme,
					&ns); err != nil {
					log.Error(err,
						"unable to make reference to active objects",
						"object",
						ns)
				} else {
					hash.Status.Objects = append(hash.Status.Objects, *objRef)
				}

				_, err := controllerutil.CreateOrUpdate(
					context.TODO(),
					r,
					&ns,
					func() error { return nil },
				)
				if err != nil {
					log.Error(err, "unable to add namespace for transform",
						"hash",
						hash,
						"namespace",
						ns)
					return ctrl.Result{}, err
				}

			case "prefix":
				plugin = &builtins.PrefixSuffixTransformerPlugin{
					Prefix: fmt.Sprintf("%s-", val),
					FieldSpecs: []ktypes.FieldSpec{
						ktypes.FieldSpec{
							Path: "metadata/name",
						},
					},
				}
			case "suffix":
				plugin = &builtins.PrefixSuffixTransformerPlugin{
					Suffix: fmt.Sprintf("-%s", val),
					FieldSpecs: []ktypes.FieldSpec{
						ktypes.FieldSpec{
							Path: "metadata/name",
						},
					},
				}
			}
			err := plugin.Transform(m)
			if err != nil {
				log.Error(err, "unable to transform")
				return ctrl.Result{}, err
			}
		}

		res := m.Resources()
		for _, v := range res {
			decode := yaml.NewYAMLOrJSONDecoder(strings.NewReader(v.String()), 10)
			var u unstruct.Unstructured
			err = decode.Decode(&u)
			if err != nil {
				log.Error(err, "unable to decode kustomize manifests")
				return ctrl.Result{}, client.IgnoreNotFound(err)
			}
			if u.GetNamespace() == "" {
				u.SetNamespace("default")
			}
			if err := ctrl.SetControllerReference(&hash, &u, r.Scheme); err != nil {
				log.Error(err, "unable to create resource for hash", "hash", hash)
				return ctrl.Result{}, err
			}

			result, err := controllerutil.CreateOrUpdate(
				context.TODO(),
				r,
				&u,
				func() error { return nil },
			)
			if err != nil &&
				(!errors.IsAlreadyExists(err) &&
					result != controllerutil.OperationResultUpdated) {
				log.Error(err, "unable to create object for hash", "object", u)
				return ctrl.Result{}, err
			}
			log.V(1).Info("Operation result", string(result), "Object for Hash", "object", u)

			objRef, err := ref.GetReference(r.Scheme, &u)
			if err != nil {
				log.Error(err, "unable to make reference to active objects", "object", u)
			} else {
				hash.Status.Objects = append(hash.Status.Objects, *objRef)
			}
			log.V(1).Info("object for Hash", "object", u)

		}
	}
	if err := r.Status().Update(ctx, &hash); err != nil {
		log.Error(err, "unable to update Hash status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *HashReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gitv1.Hash{}).
		Complete(r)
}
