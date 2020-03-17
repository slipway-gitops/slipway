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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HashSpec defines the desired state of Hash
type HashSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Reference to parent GitRepo CRD.

	// +kubebuilder:validation:Required
	GitRepo string `json:"gitrepo"`

	// Operations to perform
	// +kubebuilder:validation:Required
	// +listType:=atomic
	Operations []Operation `json:"operations"`
}

// HashStatus defines the observed state of Hash
type HashStatus struct {
	// A list of pointers to current deployed objects.
	// +optional
	Objects []corev1.ObjectReference `json:"active,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=hashes,scope=Cluster
// +kubebuilder:subresource:status
// Hash is the Schema for the hashes API
type Hash struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HashSpec   `json:"spec,omitempty"`
	Status HashStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HashList contains a list of Hash
type HashList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Hash `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Hash{}, &HashList{})
}
