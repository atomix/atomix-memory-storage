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

package controller

import (
	"context"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/atomix/cache-storage-controller/pkg/apis/storage/v1beta1"
	"github.com/atomix/kubernetes-controller/pkg/apis/cloud/v1beta2"
	"github.com/atomix/kubernetes-controller/pkg/controller/v1beta2/util/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var log = logf.Log.WithName("cache_storage_controller")

// Add creates a new Partition ManagementGroup and adds it to the Manager. The Manager will set fields on the ManagementGroup
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	log.Info("Add manager")

	r := &Reconciler{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}

	// Create a new controller
	c, err := controller.New(mgr.GetScheme().Name(), mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the storage resource and enqueue Clusters that reference it
	err = c.Watch(&source.Kind{Type: &v1beta1.CacheStorageClass{}}, &handler.EnqueueRequestsFromMapFunc{
		ToRequests: &clusterMapper{
			client: r.client,
		},
	})
	if err != nil {
		return err
	}

	// Watch for changes to referencing resource Clusters
	err = c.Watch(&source.Kind{Type: &v1beta2.Cluster{}}, &handler.EnqueueRequestsFromMapFunc{
		ToRequests: &storageFilter{
			client: r.client,
		},
	})
	if err != nil {
		return err
	}
	return nil
}

var _ reconcile.Reconciler = &Reconciler{}

// Reconciler reconciles a Cluster object
type Reconciler struct {
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Cluster object and makes changes based on the state read
// and what is in the Cluster.Spec
func (r *Reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile Cluster")
	cluster := &v1beta2.Cluster{}
	err := r.client.Get(context.TODO(), request.NamespacedName, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{Requeue: true}, err
	}

	storage := &v1beta1.CacheStorageClass{}
	namespace := cluster.Spec.Storage.Namespace
	if namespace == "" {
		namespace = cluster.Namespace
	}
	name := types.NamespacedName{
		Namespace: namespace,
		Name:      cluster.Spec.Storage.Name,
	}
	err = r.client.Get(context.TODO(), name, storage)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{Requeue: true}, err
	}

	err = r.reconcileConfigMap(cluster, storage)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.reconcileDeployment(cluster, storage)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.reconcileService(cluster, storage)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.reconcileStatus(cluster, storage)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *Reconciler) reconcileConfigMap(cluster *v1beta2.Cluster, storage *v1beta1.CacheStorageClass) error {
	log.Info("Reconcile cache storage config map")
	cm := &corev1.ConfigMap{}
	name := types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}
	err := r.client.Get(context.TODO(), name, cm)
	if err != nil && k8serrors.IsNotFound(err) {
		err = r.addConfigMap(cluster, storage)
	}
	return err
}

func (r *Reconciler) reconcileDeployment(cluster *v1beta2.Cluster, storage *v1beta1.CacheStorageClass) error {
	log.Info("Reconcile cache storage deployment")
	dep := &appsv1.Deployment{}
	name := types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}
	err := r.client.Get(context.TODO(), name, dep)
	if err != nil && k8serrors.IsNotFound(err) {
		err = r.addDeployment(cluster, storage)
	}
	return err
}

func (r *Reconciler) reconcileService(cluster *v1beta2.Cluster, storage *v1beta1.CacheStorageClass) error {
	log.Info("Reconcile cache storage service")
	service := &corev1.Service{}
	name := types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}
	err := r.client.Get(context.TODO(), name, service)
	if err != nil && k8serrors.IsNotFound(err) {
		err = r.addService(cluster, storage)
	}
	return err
}

func (r *Reconciler) reconcileStatus(cluster *v1beta2.Cluster, storage *v1beta1.CacheStorageClass) error {
	dep := &appsv1.Deployment{}
	name := types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}
	err := r.client.Get(context.TODO(), name, dep)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if cluster.Status.ReadyPartitions < cluster.Spec.Partitions &&
		dep.Status.ReadyReplicas == dep.Status.Replicas {
		clusterID, err := k8s.GetClusterIDFromClusterAnnotations(cluster)
		if err != nil {
			return err
		}
		for partitionID := (cluster.Spec.Partitions * (clusterID - 1)) + 1; partitionID <= cluster.Spec.Partitions*clusterID; partitionID++ {
			partition := &v1beta2.Partition{}
			err := r.client.Get(context.TODO(), k8s.GetPartitionNamespacedName(cluster, partitionID), partition)
			if err != nil && !k8serrors.IsNotFound(err) {
				return err
			}
			if !partition.Status.Ready {
				partition.Status.Ready = true
				log.Info("Updating Partition status", "Name", partition.Name, "Namespace", partition.Namespace, "Ready", partition.Status.Ready)
				err = r.client.Status().Update(context.TODO(), partition)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// clusterMapper is a request mapper that triggers the reconciler for referencing Clusters
// when a RaftStorageClass is changed
type clusterMapper struct {
	client client.Client
}

func (m *clusterMapper) Map(object handler.MapObject) []reconcile.Request {
	// Find all clusters that reference the changed storage
	clusters := &v1beta2.ClusterList{}
	err := m.client.List(context.TODO(), clusters, &client.ListOptions{})
	if err != nil {
		return []reconcile.Request{}
	}

	// Iterate through clusters and requeue any that reference the storage controller
	requests := []reconcile.Request{}
	for _, cluster := range clusters.Items {
		if cluster.Spec.Storage.Group == v1beta1.CacheStorageClassGroup &&
			cluster.Spec.Storage.Version == v1beta1.CacheStorageClassVersion &&
			cluster.Spec.Storage.Kind == v1beta1.CacheStorageClassKind &&
			cluster.Spec.Storage.Name == object.Meta.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: cluster.GetNamespace(),
					Name:      cluster.GetName(),
				},
			})
		}
	}
	return requests
}

// storageFilter is a request mapper that triggers the reconciler for the storage controller when
// referenced by a watched Cluster
type storageFilter struct {
	client client.Client
}

func (m *storageFilter) Map(object handler.MapObject) []reconcile.Request {
	cluster := object.Object.(*v1beta2.Cluster)

	// If the Cluster references a CacheStorageClass, enqueue the request
	if cluster.Spec.Storage.Group == v1beta1.CacheStorageClassGroup &&
		cluster.Spec.Storage.Version == v1beta1.CacheStorageClassVersion &&
		cluster.Spec.Storage.Kind == v1beta1.CacheStorageClassKind {
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: cluster.GetNamespace(),
					Name:      cluster.GetName(),
				},
			},
		}
	}
	return []reconcile.Request{}
}
