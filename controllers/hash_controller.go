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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstruct "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	ref "k8s.io/client-go/tools/reference"

	"sigs.k8s.io/kustomize/api/builtins"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"
	ktypes "sigs.k8s.io/kustomize/api/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gitv1 "github.com/slipway-gitops/slipway/api/v1"
)

// HashReconciler reconciles a Hash object
type HashReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

var (
	annotationsPlugin = &builtins.AnnotationsTransformerPlugin{
		Annotations: make(map[string]string),
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
	imagesPlugin = &builtins.ImageTagTransformerPlugin{
		ImageTag: ktypes.Image{
			Name:   "",
			NewTag: "",
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

	labelsPlugin = &builtins.LabelTransformerPlugin{
		Labels: make(map[string]string),
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
	namespacePlugin = &builtins.NamespaceTransformerPlugin{
		ObjectMeta: ktypes.ObjectMeta{
			Namespace: "",
		},
		FieldSpecs: []ktypes.FieldSpec{
			ktypes.FieldSpec{
				Path:               "metadata/namespace",
				CreateIfNotPresent: true,
			},
		},
	}
	prefixSuffixPlugin = &builtins.PrefixSuffixTransformerPlugin{
		FieldSpecs: []ktypes.FieldSpec{
			ktypes.FieldSpec{
				Path: "metadata/name",
			},
		},
	}
)

// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=hashes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=hashes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=gitrepoes,verbs=get
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=hashes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=*

func (r *HashReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {

	ctx := context.Background()
	log := r.Log.WithValues("hash", req.NamespacedName)

	// Get originating Hash
	var hash gitv1.Hash
	if err := r.Get(ctx, req.NamespacedName, &hash); err != nil {
		log.Error(err, "unable to fetch Hash", "request", req)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Get GitRepo parent of Hash
	var gitrepo gitv1.GitRepo
	if err := r.Get(ctx, types.NamespacedName{Name: hash.Spec.GitRepo}, &gitrepo); err != nil {
		log.Error(err, "unable to fetch Owner Repo", "repo", hash.Spec.GitRepo)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Filesystem needed for Kustomize to make a call
	fs := filesys.MakeFsOnDisk()

	// Instantiate a kustomizer
	opts := krusty.MakeDefaultOptions()
	k := krusty.MakeKustomizer(fs, opts)

	// sort all the Operations in this Hash by weight
	sort.Slice(hash.Spec.Operations, func(i, j int) bool {
		return *hash.Spec.Operations[i].Weight < *hash.Spec.Operations[j].Weight
	})

	// Status objects will be reset and be set at the end of reconciliation
	hash.Status.Objects = nil

	// Range over all the sorted operations
	for _, operation := range hash.Spec.Operations {

		// If HashSpec is set, set the reference to the end of the url
		path := operation.Path
		if operation.HashPath {
			path = fmt.Sprintf("%v?ref=%v", path, hash.Name)
		}

		// Run Kustomize returns ResMap
		// https://godoc.org/sigs.k8s.io/kustomize/api/resmap#ResMap
		m, err := k.Run(path)
		if err != nil {
			log.Error(err, "unable to fetch kustomize manifests", "operation", operation)
			return ctrl.Result{}, err
		}

		// Run all transformers against the ResMap
		for _, t := range operation.Transformers {

			// This sets the value for the "key":"value" for the transformer
			var val string
			switch t.Value {
			case "branch", "pull", "tag":
				val = operation.ReferenceTitle
			case "hash":
				val = hash.Name
			default:
				val = t.Value
			}
			var err error
			switch t.Type {
			case "annotations":
				plugin := *annotationsPlugin
				plugin.Annotations[t.Key] = val
				err = plugin.Transform(m)
			case "images":
				plugin := *imagesPlugin
				plugin.ImageTag.Name = t.Key
				plugin.ImageTag.NewTag = val
				err = plugin.Transform(m)
			case "labels":
				plugin := *labelsPlugin
				plugin.Labels[t.Key] = val
				err = plugin.Transform(m)
			case "namespace":
				plugin := *namespacePlugin
				plugin.ObjectMeta.Namespace = val
				err = plugin.Transform(m)
				// Create or update a namespace,
				// this will create or update later if already in the manifest
				ns := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: val},
				}
				// Take ownership
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
				// add the reference to the status
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
				// Creat or update the namespace
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
				plugin := *prefixSuffixPlugin
				plugin.Prefix = fmt.Sprintf("%s-", val)
				err = plugin.Transform(m)
			case "suffix":
				plugin := *prefixSuffixPlugin
				plugin.Suffix = fmt.Sprintf("-%s", val)
				err = plugin.Transform(m)
			}
			// run the transformer against the ResMap
			if err != nil {
				log.Error(err, "unable to transform")
				return ctrl.Result{}, err
			}
		}

		//  Loop through all the kustomize objects from the ResMap
		res := m.Resources()
		for _, v := range res {
			// move it to Yaml and decode to kuberentes unstructured objects
			decode := yaml.NewYAMLOrJSONDecoder(strings.NewReader(v.String()), 10)
			var u unstruct.Unstructured
			err = decode.Decode(&u)
			if err != nil {
				log.Error(err, "unable to decode kustomize manifests")
				return ctrl.Result{}, client.IgnoreNotFound(err)
			}
			// unstructured namespaced objects just through an error with namespace empty
			if u.GetNamespace() == "" {
				u.SetNamespace("default")
			}
			// Take ownership of the resource
			if err := ctrl.SetControllerReference(&hash, &u, r.Scheme); err != nil {
				log.Error(err, "unable to create resource for hash", "hash", hash)
				return ctrl.Result{}, err
			}
			// Create or Update
			result, err := controllerutil.CreateOrUpdate(
				context.TODO(),
				r,
				&u,
				func() error { return nil },
			)
			// Catch specific errors for CreateOrUpdate
			if err != nil &&
				(!errors.IsAlreadyExists(err) &&
					result != controllerutil.OperationResultUpdated) {
				log.Error(err, "unable to create object for hash", "object", u)
				return ctrl.Result{}, err
			}

			log.Info("Operation result", string(result), "Object for Hash", "object", u)

			// Safe the reference in status
			objRef, err := ref.GetReference(r.Scheme, &u)
			if err != nil {
				log.Error(err, "unable to make reference to active objects", "object", u)
			} else {
				hash.Status.Objects = append(hash.Status.Objects, *objRef)
			}
			log.Info("object for Hash", "object", u)

		}
	}
	// Set the Hash status
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
