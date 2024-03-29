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

package apis

import (
	storagev1beta1 "github.com/atomix/atomix-memory-storage/pkg/apis/storage/v1beta1"
	atomixv1beta3 "github.com/atomix/kubernetes-controller/pkg/apis/cloud/v1beta3"
)

func init() {
	// register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes, storagev1beta1.SchemeBuilder.AddToScheme)
	AddToSchemes = append(AddToSchemes, atomixv1beta3.SchemeBuilder.AddToScheme)
}
