// Copyright 2020-present Open Networking Foundation.
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

package v2beta1

import (
	"github.com/atomix/atomix-go-framework/pkg/atomix/logging"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var log = logging.GetLogger("atomix", "controller", "raft")

// AddControllers creates a new Partition ManagementGroup and adds it to the Manager. The Manager will set fields on the ManagementGroup
// and Start it when the Manager is Started.
func AddControllers(mgr manager.Manager) error {
	if err := addMemoryStoreController(mgr); err != nil {
		return err
	}
	return nil
}
