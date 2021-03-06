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
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/Masterminds/semver/v3"
	gitv1 "github.com/slipway-gitops/slipway/api/v1"
	"github.com/slipway-gitops/slipway/pkg/gitpath"
	gitclient "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	gitssh "gopkg.in/src-d/go-git.v4/plumbing/transport/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These are just for github bb and gitlab may behave differently
var (
	ownerKey = ".metadata.controller"
	apiGVStr = gitv1.GroupVersion.String()
)

// GitRepoReconciler reconciles a GitRepo object
type GitRepoReconciler struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	recorder   record.EventRecorder
	gitpaths   map[string]gitpath.GitPath
	PluginPath string
}

type highestTagSpec struct {
	Version *semver.Version
	Hash    string
}

// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=gitrepos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=git.gitops.slipway.org,resources=gitrepos/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
func (r *GitRepoReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("gitrepo", req.NamespacedName)
	dur, _ := time.ParseDuration("1m")
	returnResult := ctrl.Result{RequeueAfter: dur}
	// Get the referenced GitRepo
	var repo gitv1.GitRepo
	if err := r.Get(ctx, req.NamespacedName, &repo); err != nil {
		log.Error(err, "unable to fetch Repo")
		return returnResult, client.IgnoreNotFound(err)
	}
	// Get the remote git repo
	remote := getRemote(repo)
	// Get the Auth key from ~/.ssh/id_rsa
	auth, err := getSSHKeyAuth()
	if err != nil {
		log.Error(err, "Unable to parse ssh key")
		return returnResult, nil
	}
	// Get all git references like git ls-remote
	refs, err := remote.List(&gitclient.ListOptions{Auth: auth})
	if err != nil {
		log.Error(err, "remote access error")
		return returnResult, nil
	}

	// activeHashes will be a map of the [commithash] and the HashSpec that applies to it.
	activeHashes := make(map[string]*gitv1.HashSpec)

	// default is github
	// TODO: move this and the maps to the packages
	if repo.Spec.GitPath == "" {
		repo.Spec.GitPath = "github"
	}
	gitpath, ok := r.gitpaths[repo.Spec.GitPath]
	if !ok {
		log.Error(err, "No plugin for this gitpath type")
		return returnResult, nil
	}
	// Range over every operation and if it matches the "optype" and the reference add it to the HashSpec
OPLOOP:
	for _, op := range repo.Spec.Operations {
		// This is for "highesttag" optype
		highestTag := highestTagSpec{}
		// Go through every reference in the op to see if you should add this op to the
		// Hashspec
		for _, ref := range refs {
			// Ignore HEAD, we are not doing that
			if ref.Name().String() != "HEAD" {
				gp, err := gitpath.New(string(op.Type), op.Reference, ref.Name().String())
				if err != nil {
					log.Error(err, "Unable to load gitpath", "gitpath", gp)
					return returnResult, err
				}
				// op.ReferenceTitle is the human readable version of the title
				// PRs are pull-#
				// Branches and Tags are just the branch name
				op.ReferenceTitle = gp.Title()
				// Does the op reference match?
				if gp.Match() {
					// instantiate empty transformers, this should be moved
					if op.Transformers == nil {
						op.Transformers = []gitv1.Transformer{}
					}
					// If highesttag and is highest semver tag save it
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
						} else {
							current, err := semver.NewVersion(op.ReferenceTitle)
							if err != nil {
								log.Error(err, "unable parse semver, Skipping Op", "op", op)
								continue OPLOOP
							}
							highestTag.Version = current
							highestTag.Hash = ref.Hash().String()

						}
						// Create or update the HashSpec with the operations
					} else {
						if val, ok := activeHashes[ref.Hash().String()]; ok {
							val.Operations = append(val.Operations, op)
							activeHashes[ref.Hash().String()] = val
						} else {
							spec := &gitv1.HashSpec{
								GitRepo:    repo.ObjectMeta.Name,
								Operations: []gitv1.Operation{op},
								Store:      &repo.Spec.Store,
							}
							activeHashes[ref.Hash().String()] = spec
						}
					}

				}
			}
		}
		// Loop is over add the highesttag
		if op.Type == "highesttag" {
			op.ReferenceTitle = highestTag.Version.Original()
			if val, ok := activeHashes[highestTag.Hash]; ok {
				val.Operations = append(val.Operations, op)
				activeHashes[highestTag.Hash] = val
			} else {
				spec := &gitv1.HashSpec{
					GitRepo:    repo.ObjectMeta.Name,
					Operations: []gitv1.Operation{op},
					Store:      &repo.Spec.Store,
				}
				activeHashes[highestTag.Hash] = spec
			}

		}
	}
	// Retrieve all Hashes owned by this GitRepo
	var runningHashes gitv1.HashList
	if err := r.List(ctx,
		&runningHashes,
		client.InNamespace(req.Namespace),
		client.MatchingFields{ownerKey: req.Name},
	); err != nil {
		log.Error(err, "unable to list child Hashes")
		return returnResult, err
	}
	// Reset Status to nil.  Will be saved at the end
	repo.Status.Hashes = nil
	for _, runningHash := range runningHashes.Items {
		// If the running hash is present in the activehashes update it.  If not delete it.
		if val, ok := activeHashes[runningHash.Name]; ok {
			runningHash.Spec = *val
			// Create or update the hash
			err := r.Update(
				ctx,
				&runningHash,
			)
			if err != nil {
				log.Error(err, "unable to create hash for GitRepo", "hash", runningHash)
				return returnResult, err
			}
			log.Info("Updated hash for GitRepo", "hash", runningHash)

			r.recorder.Event(
				&repo,
				"Normal",
				"update",
				fmt.Sprintf("Repo update for hash %s", runningHash.Name),
			)
			// Add the object to the status
			objRef, err := ref.GetReference(r.Scheme, &runningHash)
			if err != nil {
				log.Error(err, "unable to make reference to active objects", "hash", runningHash)
			} else {
				repo.Status.Hashes = append(repo.Status.Hashes, *objRef)
			}
		} else {
			// Hash is no longer referenced delete it.
			if err := r.Delete(
				ctx,
				&runningHash,
				client.PropagationPolicy(metav1.DeletePropagationBackground),
			); err == nil {
				log.Info("deleted old hash failed", "hash", runningHash)
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
		// delete from active hashes list.  Whatever is left never existed
		delete(activeHashes, runningHash.Name)

	}
	// Loop through remaining active hashes and createThe hash
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
		log.Info("Operation result", string(result), "Hash for GitRepo", "hash", hash)

	}
	// Save the status
	if err := r.Status().Update(ctx, &repo); err != nil {
		log.Error(err, "unable to update Repo status")
		return returnResult, err
	}
	return returnResult, nil
}

func (r *GitRepoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	r.gitpaths, err = gitpath.LoadGitPaths(fmt.Sprintf("%s/gitpaths/", r.PluginPath))
	if err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().IndexField(&gitv1.Hash{}, ownerKey, func(rawObj runtime.Object) []string {
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
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func getRemote(g gitv1.GitRepo) (remote *gitclient.Remote) {
	storer := gitclientmem.NewStorage()
	remote = gitclient.NewRemote(storer, &gitclientconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{g.Spec.Uri},
	})
	return
}

func getSSHKeyAuth() (auth transport.AuthMethod, err error) {
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
