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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GitRepoSpec defines the desired state of GitRepo
type GitRepoSpec struct {

	// Uri is the location of the repo.
	Uri string `json:"uri"`

	// GitPath determines how references should be parsed
	// See https://github.com/slipway-gitops/slipway#the-spec
	// +optional
	GitPath string `json:"gitpath"`

	// Store is a location to store operation artifacts after they have been released
	// +optional
	Store `json:"store,omitempty"`

	// Operations: list of Operations
	Operations []Operation `json:"operations"`
}

// Store defines a cloud object store.
type Store struct {

	// Cloud provider: aws, azure, gcp
	Cloud string `json:"cloud"`

	// Bucket
	Bucket string `json:"bucket"`
}

// Operation defines how you should react to new Hash CRDS.
type Operation struct {
	// Name of the operation.
	Name string `json:"operation"`
	// Path to kustomize files.
	Path string `json:"path"`
	// HashPath adds a kustomize ref of the commit hash to the end of the Path
	// +optional
	HashPath bool `json:"hashpath"`
	// Weight to determin order
	// +optional
	Weight *int64 `json:"weight,omitempty"`
	// Type of Operation
	// kubebuilder:validation:MinLength=1
	Type OpType `json:"optype"`
	// Type Reference
	// +optional
	Reference string `json:"reference"`
	// Type Reference
	// +optional
	ReferenceTitle string `json:"referencetitle"`
	// Type tranformers
	// +optional
	Transformers []Transformer `json:"transformers"`
}

// OpType is the type of operation that will take place
// +kubebuilder:validation:Enum=tag;branch;pull
type OpType string

// Transformers are kustomize transformers available for contextual
// transformation that cannot be accomplished with normal kustomize manifests
type Transformer struct {
	// Type of tranformer valid types annotations, images, labels, namespace, prefix, suffix
	Type string `json:"type"`
	// Value to use with transformer valid types are hash, pull, branch, tag
	Value string `json:"value"`
	// Key value for tools like labels and annotations
	// +optional
	Key string `json:"key"`
}

// GitRepoStatus defines the observed state of GitRepo
type GitRepoStatus struct {
	// Information when was the last time the git repo was scanned.
	// +optional
	LastSync *metav1.Time `json:"lastSync,omitempty"`
	// A list of pointers to all associated Hash CRDS.
	// +optional
	Hashes []corev1.ObjectReference `json:"Sha,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=gitrepos,scope=Cluster
// +kubebuilder:subresource:status
// GitRepo is the Schema for the gitrepos API
type GitRepo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              GitRepoSpec   `json:"spec,omitempty"`
	Status            GitRepoStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// GitRepoList contains a list of GitRepo
type GitRepoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitRepo `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitRepo{}, &GitRepoList{})
}
