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
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	unstruct "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/record"
	ref "k8s.io/client-go/tools/reference"

	"sigs.k8s.io/kustomize/api/builtins"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"
	ktypes "sigs.k8s.io/kustomize/api/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gitv1 "github.com/slipway-gitops/slipway/api/v1"
	"github.com/slipway-gitops/slipway/pkg/objectstore"
)

// HashReconciler reconciles a Hash object
type HashReconciler struct {
	client.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	PluginPath   string
	recorder     record.EventRecorder
	objectstores map[string]objectstore.ObjectStore
	watcher      func(*unstruct.Unstructured, *gitv1.Hash) error
}

var (
	ErrEmptyPath      = errors.New("No path set")
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
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=gitrepos,verbs=get
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=hashes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=*
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

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

	// If storage is set
	// TODO: move this and the maps to the packages
	var storage objectstore.ObjectStore
	if hash.Spec.Store != nil && hash.Spec.Store.Type != "" {
		if val, ok := r.objectstores[hash.Spec.Store.Type]; ok {
			storage = val.New(hash.Spec.Store.Bucket)
		} else {
			log.Error(objectstore.ErrInvalidType, "No plugin for this objectstore type", "store", hash.Spec.Store)
			return ctrl.Result{}, nil
		}
	} else {
		log.Info("No Storage type selected")
	}
	// Filesystem needed for Kustomize to make a call
	fs := filesys.MakeFsOnDisk()

	// Instantiate a kustomizer
	opts := krusty.MakeDefaultOptions()
	k := krusty.MakeKustomizer(fs, opts)

	// sort all the Operations in this Hash by weight
	sort.Slice(hash.Spec.Operations, func(i, j int) bool {
		return hash.Spec.Operations[i].Weight < hash.Spec.Operations[j].Weight
	})

	// Save old objects and delete ones that are no longer present.
	oldObjects := hash.Status.Objects
	// Status objects will be reset and be set at the end of reconciliation
	hash.Status.Objects = nil

	// Range over all the sorted operations
	for _, operation := range hash.Spec.Operations {

		if operation.Path == "" {
			log.Error(ErrEmptyPath, "Invalid path", "operation", operation)
			continue
		}
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
				if err := controllerutil.SetControllerReference(
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
				// Creat or update the namespace
				result, err := controllerutil.CreateOrUpdate(
					context.TODO(),
					r,
					&ns,
					func() error { return nil },
				)
				log.Info("Operation result", string(result), "Object for Hash", "object", ns)
				r.recorder.Event(
					&hash,
					"Normal",
					string(result),
					fmt.Sprintf("%s Kind:%s Named:%s",
						strings.Title(string(result)),
						"Namespace",
						val,
					),
				)
				if err != nil {
					log.Error(err, "unable to add namespace for transform",
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
			if err := controllerutil.SetControllerReference(&hash, &u, r.Scheme); err != nil {
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
			r.recorder.Event(
				&hash,
				"Normal",
				string(result),
				fmt.Sprintf("%s Kind:%s Named:%s in Namespace:%s",
					strings.Title(string(result)),
					u.GetKind(),
					u.GetName(),
					u.GetNamespace(),
				),
			)
			// Catch specific errors for CreateOrUpdate
			if err != nil {
				log.Error(err, "unable to create object for hash", "object", u)
				return ctrl.Result{}, err
			}
			if err := r.watcher(&u, &hash); err != nil {
				log.Error(err, "unable to set watch on object", "object", u, "hash", hash)

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
		yaml, err := m.AsYaml()
		if err != nil {
			log.Error(err, "unable to produce yaml from resourcemap", "resourcemap", m)
		}
		if storage != nil {
			go func(hash, op string, bytes []byte) {
				err = storage.Save(hash, op, bytes)
				if err != nil {
					log.Error(err, "unable to save yaml to storage", "yaml", yaml)
				}
			}(hash.Name, operation.Name, yaml)
		}
	}
	// Reap Old references
OLDOBJECTLOOP:
	for _, oobj := range oldObjects {
		for _, nobj := range hash.Status.Objects {
			if nobj.Kind == oobj.Kind &&
				nobj.Name == oobj.Name &&
				nobj.Namespace == oobj.Namespace {
				continue OLDOBJECTLOOP
			}
		}
		u := &unstructured.Unstructured{}
		u.SetName(oobj.Name)
		u.SetNamespace(oobj.Namespace)
		u.SetGroupVersionKind(oobj.GroupVersionKind())
		err := r.Delete(ctx, u)
		if err != nil {
			log.Error(err, "unable to delete orphaned objects", "object", u)
		} else {
			r.recorder.Event(
				&hash,
				"Normal",
				"delete",
				fmt.Sprintf("%s Kind:%s Named:%s in Namespace:%s",
					"Deleted",
					u.GetKind(),
					u.GetName(),
					u.GetNamespace(),
				),
			)
			log.Info("Operation result", "delete", "Object for Hash", "object", u)
		}
	}
	if err := r.Status().Update(ctx, &hash); err != nil {
		log.Error(err, "unable to update Hash status")
	}
	return ctrl.Result{}, nil
}

func (r *HashReconciler) SetupWithManager(mgr ctrl.Manager) (err error) {
	r.objectstores, err = objectstore.LoadObjectStores(fmt.Sprintf("%s/objectstores/", r.PluginPath))
	if err != nil {
		return err
	}

	r.recorder = mgr.GetEventRecorderFor("hash-controller")

	cntrl, err := ctrl.NewControllerManagedBy(mgr).
		For(&gitv1.Hash{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Build(r)
	r.watcher = watcher(cntrl)
	return err
}

type WatchedObjPredicate struct{}

// Ignore creation
func (WatchedObjPredicate) Create(e event.CreateEvent) bool { return false }

// If objects are deleted we need to rerun
func (WatchedObjPredicate) Delete(e event.DeleteEvent) bool { return true }

// If objects are updated we need to rerun
func (WatchedObjPredicate) Update(e event.UpdateEvent) bool { return true }

// Always false, not meant for this controller
func (WatchedObjPredicate) Generic(e event.GenericEvent) bool { return false }

func watcher(cntrl controller.Controller) func(*unstruct.Unstructured, *gitv1.Hash) error {
	watcher := func(u *unstruct.Unstructured, h *gitv1.Hash) error {
		return cntrl.Watch(&source.Kind{Type: u},
			&handler.EnqueueRequestForOwner{OwnerType: h},
			WatchedObjPredicate{},
		)
	}
	return watcher
}
