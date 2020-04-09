// Copyright 2019-present Open Networking Foundation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CacheStorageClassGroup cache storage class group
const CacheStorageClassGroup = "storage.cloud.atomix.io"

// CacheStorageClassVersion cache storage class version
const CacheStorageClassVersion = "v1beta1"

// CacheStorageClassKind cache storage class kind
const CacheStorageClassKind = "CacheStorageClass"

// CacheStorageClassSpec is the k8s spec for a CacheStorage resource
type CacheStorageClassSpec struct {
	// Image is the image to run
	Image string `json:"image,omitempty"`

	// ImagePullPolicy is the pull policy to apply
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
}

// CacheStorageClassStatus defines the observed state of CacheStorage
type CacheStorageClassStatus struct {
	Ready bool `json:"ready,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CacheStorageClass is the Schema for the CacheStorage API
// +k8s:openapi-gen=true
type CacheStorageClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the CacheStorage specification
	Spec CacheStorageClassSpec `json:"spec,omitempty"`

	// Status if the current status of the CacheStorage
	Status CacheStorageClassStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CacheStorageClassList contains a list of CacheStorage
type CacheStorageClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the set of items in the list
	Items []CacheStorageClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CacheStorageClass{}, &CacheStorageClassList{})
}
