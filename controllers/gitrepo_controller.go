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
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/crypto/ssh"
	gitclientconfig "gopkg.in/src-d/go-git.v4/config"
	gitclientmem "gopkg.in/src-d/go-git.v4/storage/memory"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ref "k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/Masterminds/semver/v3"
	gitv1 "github.com/slipway-gitops/slipway/api/v1"
	gitclient "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	gitssh "gopkg.in/src-d/go-git.v4/plumbing/transport/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These are just for github bb and gitlab may behave differently
//TODO: move to git provider tooling
var (
	optypes = map[string]string{
		"pull":       `^refs/pull/[0-9]+/merge$`,
		"branch":     `^refs/heads/%v$`,
		"tags":       `^refs/tags/%v$`,
		"highesttag": `^refs/tags/%v$`,
	}
	jobOwnerKey = ".metadata.controller"
	apiGVStr    = gitv1.GroupVersion.String()
)

// GitRepoReconciler reconciles a GitRepo object
type GitRepoReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	recorder record.EventRecorder
}

type highestTagSpec struct {
	Version *semver.Version
	Hash    string
}

// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=gitrepoes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=gitrepoes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
func (r *GitRepoReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("gitrepo", req.NamespacedName)
	dur, _ := time.ParseDuration("1m")
	returnResult := ctrl.Result{RequeueAfter: dur}
	var repo gitv1.GitRepo
	if err := r.Get(ctx, req.NamespacedName, &repo); err != nil {
		log.Error(err, "unable to fetch Repo")
		return returnResult, client.IgnoreNotFound(err)
	}

	remote := GetRemote(repo)
	auth, err := get_ssh_key_auth()
	if err != nil {
		log.Error(err, "Unable to parse ssh key")
		return returnResult, nil
	}

	refs, err := remote.List(&gitclient.ListOptions{Auth: auth})
	if err != nil {
		log.Error(err, "remote access error")
		return returnResult, nil
	}
	activeHashes := make(map[string]*gitv1.HashSpec)
OPLOOP:
	for _, op := range repo.Spec.Operations {
		highestTag := highestTagSpec{}
		for _, ref := range refs {
			var regx string
			if ref.Name().String() != "HEAD" {
				if op.Type == "pull" {
					regx = optypes[op.Type]
					op.ReferenceTitle = strings.Join(strings.Split(ref.Name().String(), "/")[1:3], "-")
				} else {
					regx = fmt.Sprintf(optypes[op.Type], op.Reference)
					op.ReferenceTitle = strings.Split(ref.Name().String(), "/")[2]
				}
				match := regexp.MustCompile(regx)
				if match.MatchString(ref.Name().String()) {
					if op.Transformers == nil {
						op.Transformers = []gitv1.Transformer{}
					}
					if op.Type == "highesttag" {
						if highestTag.Version != nil {
							current, err := semver.NewVersion(op.ReferenceTitle)
							if err != nil {
								log.Error(err, "unable parse semver, Skipping Op", "op", op)
								continue OPLOOP
							}
							if current.GreaterThan(highestTag.Version) {
								highestTag.Version = current
								highestTag.Hash = ref.Hash().String()
							}
						}
					} else {
						if val, ok := activeHashes[ref.Hash().String()]; ok {
							val.Operations = append(val.Operations, op)
							activeHashes[ref.Hash().String()] = val
						} else {
							activeHashes[ref.Hash().String()] = &gitv1.HashSpec{
								GitRepo:    repo.ObjectMeta.Name,
								Operations: []gitv1.Operation{op},
							}
						}
					}

				}
			}
		}
		if op.Type == "highesttag" {
			if val, ok := activeHashes[highestTag.Hash]; ok {
				val.Operations = append(val.Operations, op)
				activeHashes[highestTag.Hash] = val
			} else {
				activeHashes[highestTag.Hash] = &gitv1.HashSpec{
					GitRepo:    repo.ObjectMeta.Name,
					Operations: []gitv1.Operation{op},
				}
			}

		}
	}
	var runningHashes gitv1.HashList
	if err := r.List(ctx,
		&runningHashes,
		client.InNamespace(req.Namespace),
		client.MatchingFields{jobOwnerKey: req.Name},
	); err != nil {
		log.Error(err, "unable to list child Hashes")
		return returnResult, err
	}
	repo.Status.Hashes = nil
	for _, runningHash := range runningHashes.Items {
		if val, ok := activeHashes[runningHash.Name]; ok {
			runningHash.Spec = *val
			result, err := controllerutil.CreateOrUpdate(
				context.TODO(),
				r,
				&runningHash,
				func() error { return nil },
			)
			if err != nil &&
				(errors.IsAlreadyExists(err) &&
					result == controllerutil.OperationResultUpdated) {
				log.Error(err, "unable to create hash for GitRepo", "hash", runningHash)
				return returnResult, err
			}
			log.V(1).Info("Operation result", string(result), "Hash for GitRepo", "hash", runningHash)

			r.recorder.Event(
				&repo,
				"Normal",
				string(result),
				fmt.Sprintf("Repo %s for hash %s", string(result), runningHash.Name),
			)

			objRef, err := ref.GetReference(r.Scheme, &runningHash)
			if err != nil {
				log.Error(err, "unable to make reference to active objects", "hash", runningHash)
			} else {
				repo.Status.Hashes = append(repo.Status.Hashes, *objRef)
			}
		} else {
			if err := r.Delete(
				ctx,
				&runningHash,
				client.PropagationPolicy(metav1.DeletePropagationBackground),
			); err != nil {
				log.V(0).Info("deleted old hash failed", "hash", runningHash)
			} else {
				log.Error(err, "deleted old hash", "hash", runningHash)
				r.recorder.Event(
					&repo,
					"Normal",
					"delete",
					fmt.Sprintf("Repo deleted hash %s", runningHash.Name),
				)
			}

		}

		delete(activeHashes, runningHash.Name)

	}
	for k, activeHash := range activeHashes {

		hash := &gitv1.Hash{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      make(map[string]string),
				Annotations: make(map[string]string),
				Name:        k,
			},
			Spec: *activeHash,
		}
		if err := ctrl.SetControllerReference(&repo, hash, r.Scheme); err != nil {
			log.Error(err, "unable to create hash for GitRepo", "hash", hash)
			return returnResult, err
		}
		result, err := controllerutil.CreateOrUpdate(
			context.TODO(),
			r,
			hash,
			func() error { return nil },
		)
		if err != nil &&
			(!errors.IsAlreadyExists(err) &&
				result == controllerutil.OperationResultUpdated) {
			log.Error(err, "unable to create hash for GitRepo", "hash", hash)
			return returnResult, err
		}
		r.recorder.Event(
			&repo,
			"Normal",
			string(result),
			fmt.Sprintf("Repo %s hash %s", string(result), hash.Name),
		)

		objRef, err := ref.GetReference(r.Scheme, hash)
		if err != nil {
			log.Error(err, "unable to make reference to active objects", "hash", hash)
		} else {
			repo.Status.Hashes = append(repo.Status.Hashes, *objRef)
		}
		log.V(1).Info("Operation result", string(result), "Hash for GitRepo", "hash", hash)

	}

	if err := r.Status().Update(ctx, &repo); err != nil {
		log.Error(err, "unable to update Repo status")
		return returnResult, err
	}
	return returnResult, nil
}

func (r *GitRepoReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(&gitv1.Hash{}, jobOwnerKey, func(rawObj runtime.Object) []string {
		hash := rawObj.(*gitv1.Hash)
		owner := metav1.GetControllerOf(hash)
		if owner == nil {
			return nil
		}
		if owner.APIVersion != apiGVStr || owner.Kind != "GitRepo" {
			return nil
		}

		return []string{owner.Name}
	}); err != nil {
		return err
	}
	r.recorder = mgr.GetEventRecorderFor("gitrepo-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&gitv1.GitRepo{}).
		Owns(&gitv1.Hash{}).
		Complete(r)
}

func GetRemote(g gitv1.GitRepo) (remote *gitclient.Remote) {
	storer := gitclientmem.NewStorage()
	remote = gitclient.NewRemote(storer, &gitclientconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{g.Spec.Uri},
	})
	/*
		dir, err := ioutil.TempDir("", "clone-example")
		if err != nil {
			return
		}
		defer os.RemoveAll(dir)
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		auth, err := get_ssh_key_auth(fmt.Sprintf("%s/slipwaykey", home))
		if err != nil {
			return
		}
		rep, err := gitclient.PlainClone(dir, true, &git.CloneOptions{
			URL:  g.Spec.Uri,
			Auth: auth,
		})
		if err != nil {
			return
		}
		remote, err = rep.Remote("origin")

	*/
	return
}

func get_ssh_key_auth() (auth transport.AuthMethod, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	privateSshKeyFile := fmt.Sprintf("%s/.ssh/id_rsa", home)
	sshKey, err := ioutil.ReadFile(privateSshKeyFile)
	if err != nil {
		return auth, err
	}
	signer, err := ssh.ParsePrivateKey([]byte(sshKey))
	if err != nil {
		return auth, err
	}
	auth = &gitssh.PublicKeys{User: "git", Signer: signer}
	return auth, nil
}
