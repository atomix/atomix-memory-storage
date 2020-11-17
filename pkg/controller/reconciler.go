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
	"fmt"
	api "github.com/atomix/api/proto/atomix/database"
	"github.com/atomix/cache-storage-controller/pkg/apis/storage/v1beta1"
	"github.com/atomix/kubernetes-controller/pkg/apis/cloud/v1beta3"
	"github.com/atomix/kubernetes-controller/pkg/controller/util/k8s"
	"github.com/gogo/protobuf/jsonpb"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"math"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	configVolume = "config"
)

const (
	apiPort = 5678
)

const (
	configPath         = "/etc/atomix"
	clusterConfigFile  = "cluster.json"
	protocolConfigFile = "protocol.json"
)

const (
	nodeLabel = "node"
)

const (
	defaultImage = "atomix/cache-storage-node:v0.5.1"
)

var log = logf.Log.WithName("cache_storage_controller")

var _ reconcile.Reconciler = &Reconciler{}

// Reconciler reconciles a Cluster object
type Reconciler struct {
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Cluster object and makes changes based on the state read
// and what is in the Cluster.Spec
func (r *Reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile Database")
	database := &v1beta3.Database{}
	err := r.client.Get(context.TODO(), request.NamespacedName, database)
	if err != nil {
		log.Error(err, "Reconcile Database")
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{Requeue: true}, err
	}

	log.Info("Reconcile CacheStorageClass")
	storage := &v1beta1.CacheStorageClass{}
	namespace := database.Spec.StorageClass.Namespace
	if namespace == "" {
		namespace = database.Namespace
	}
	name := types.NamespacedName{
		Namespace: namespace,
		Name:      database.Spec.StorageClass.Name,
	}
	err = r.client.Get(context.TODO(), name, storage)
	if err != nil {
		log.Error(err, "Reconcile CacheStorageClass")
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{Requeue: true}, err
	}

	log.Info("Reconcile Clusters")
	err = r.reconcileNodes(database, storage)
	if err != nil {
		log.Error(err, "Reconcile Clusters")
		return reconcile.Result{}, err
	}

	log.Info("Reconcile Partitions")
	err = r.reconcilePartitions(database, storage)
	if err != nil {
		log.Error(err, "Reconcile Partitions")
		return reconcile.Result{}, err
	}

	log.Info("Reconcile Status")
	err = r.reconcileStatus(database, storage)
	if err != nil {
		log.Error(err, "Reconcile Status")
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *Reconciler) reconcileNodes(database *v1beta3.Database, storage *v1beta1.CacheStorageClass) error {
	nodes := getNodes(database, storage)
	for node := 1; node <= nodes; node++ {
		err := r.reconcileNode(database, storage, node)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcileNode(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, node int) error {
	err := r.reconcileConfigMap(database, storage, node)
	if err != nil {
		return err
	}

	err = r.reconcileDeployment(database, storage, node)
	if err != nil {
		return err
	}

	err = r.reconcileService(database, storage, node)
	if err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) reconcileConfigMap(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, node int) error {
	log.Info("Reconcile cache storage config map")
	cm := &corev1.ConfigMap{}
	name := types.NamespacedName{
		Namespace: database.Namespace,
		Name:      getNodeName(database, node),
	}
	err := r.client.Get(context.TODO(), name, cm)
	if err != nil && k8serrors.IsNotFound(err) {
		err = r.addConfigMap(database, storage, node)
	}
	return err
}

func (r *Reconciler) addConfigMap(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, node int) error {
	log.Info("Creating ConfigMap", "Name", getNodeName(database, node), "Namespace", database.Namespace)
	config, err := newClusterConfig(database, storage, node)
	if err != nil {
		return err
	}

	marshaller := jsonpb.Marshaler{}
	data, err := marshaller.MarshalToString(config)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: database.Namespace,
			Name:      getNodeName(database, node),
			Labels:    newNodeLabels(database, node),
		},
		Data: map[string]string{
			clusterConfigFile: data,
		},
	}
	if err := controllerutil.SetControllerReference(database, cm, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), cm)
}

// newNodeConfigString creates a node configuration string for the given cluster
func newClusterConfig(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, node int) (*api.DatabaseConfig, error) {
	replicas := []api.ReplicaConfig{
		{
			ID:           getNodeName(database, node),
			Host:         fmt.Sprintf("%s.%s.svc.cluster.local", getNodeName(database, node), database.Namespace),
			ProtocolPort: apiPort,
			APIPort:      apiPort,
		},
	}

	partitions := make([]api.PartitionId, 0, database.Spec.Partitions)
	for partitionID := 1; partitionID <= int(database.Spec.Partitions); partitionID++ {
		if getNodeForPartitionID(database, storage, partitionID) == node {
			partition := api.PartitionId{
				Partition: int32(partitionID),
			}
			partitions = append(partitions, partition)
		}
	}

	return &api.DatabaseConfig{
		Replicas:   replicas,
		Partitions: partitions,
	}, nil
}

func (r *Reconciler) reconcileDeployment(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, node int) error {
	log.Info("Reconcile cache storage deployment")
	dep := &appsv1.Deployment{}
	name := types.NamespacedName{
		Namespace: database.Namespace,
		Name:      getNodeName(database, node),
	}
	err := r.client.Get(context.TODO(), name, dep)
	if err != nil && k8serrors.IsNotFound(err) {
		err = r.addDeployment(database, storage, node)
	}
	return err
}

func (r *Reconciler) addDeployment(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, node int) error {
	log.Info("Creating Deployment", "Name", getNodeName(database, node), "Namespace", database.Namespace)
	var replicas int32 = 1
	image := storage.Spec.Image
	if image == "" {
		image = defaultImage
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: database.Namespace,
			Name:      getNodeName(database, node),
			Labels:    newNodeLabels(database, node),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: newNodeLabels(database, node),
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      getNodeName(database, node),
					Namespace: database.Namespace,
					Labels:    newNodeLabels(database, node),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            storage.Name,
							Image:           image,
							ImagePullPolicy: storage.Spec.ImagePullPolicy,
							Args: []string{
								"$(NODE_ID)",
								fmt.Sprintf("%s/%s", configPath, clusterConfigFile),
								fmt.Sprintf("%s/%s", configPath, protocolConfigFile),
							},
							Env: []corev1.EnvVar{
								{
									Name: "NODE_ID",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      configVolume,
									MountPath: configPath,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: configVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: getNodeName(database, node),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(database, dep, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), dep)
}

func (r *Reconciler) reconcileService(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, node int) error {
	log.Info("Reconcile cache storage service")
	service := &corev1.Service{}
	name := types.NamespacedName{
		Namespace: database.Namespace,
		Name:      getNodeName(database, node),
	}
	err := r.client.Get(context.TODO(), name, service)
	if err != nil && k8serrors.IsNotFound(err) {
		err = r.addService(database, storage, node)
	}
	return err
}

func (r *Reconciler) addService(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, node int) error {
	log.Info("Creating service", "Name", getNodeName(database, node), "Namespace", database.Namespace)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: database.Namespace,
			Name:      getNodeName(database, node),
			Labels:    database.Labels,
		},

		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "api",
					Port: apiPort,
				},
			},
			PublishNotReadyAddresses: true,
			ClusterIP:                "None",
			Selector:                 newNodeLabels(database, node),
		},
	}
	if err := controllerutil.SetControllerReference(database, service, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), service)
}

func (r *Reconciler) reconcilePartitions(database *v1beta3.Database, storage *v1beta1.CacheStorageClass) error {
	options := &client.ListOptions{
		Namespace:     database.Namespace,
		LabelSelector: labels.SelectorFromSet(k8s.GetPartitionLabelsForDatabase(database)),
	}
	partitions := &v1beta3.PartitionList{}
	err := r.client.List(context.TODO(), partitions, options)
	if err != nil {
		return err
	}

	for _, partition := range partitions.Items {
		err := r.reconcilePartition(database, storage, partition)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcilePartition(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, partition v1beta3.Partition) error {
	service := &corev1.Service{}
	name := types.NamespacedName{
		Namespace: partition.Namespace,
		Name:      partition.Spec.ServiceName,
	}
	err := r.client.Get(context.TODO(), name, service)
	if err == nil || !k8serrors.IsNotFound(err) {
		return err
	}

	node := getNodeForPartition(database, storage, &partition)
	service = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   partition.Namespace,
			Name:        partition.Spec.ServiceName,
			Labels:      partition.Labels,
			Annotations: partition.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: newNodeLabels(database, node),
			Ports: []corev1.ServicePort{
				{
					Name: "api",
					Port: 5678,
				},
			},
		},
	}
	return r.client.Create(context.TODO(), service)
}

func (r *Reconciler) reconcileStatus(database *v1beta3.Database, storage *v1beta1.CacheStorageClass) error {
	options := &client.ListOptions{
		Namespace:     database.Namespace,
		LabelSelector: labels.SelectorFromSet(k8s.GetPartitionLabelsForDatabase(database)),
	}
	partitions := &v1beta3.PartitionList{}
	err := r.client.List(context.TODO(), partitions, options)
	if err != nil {
		return err
	}

	for _, partition := range partitions.Items {
		if !partition.Status.Ready {
			log.Info("Reconcile status", "Database", database.Name, "Partition", partition.Name, "ReadyPartitions")
			node := getNodeForPartition(database, storage, &partition)

			dep := &appsv1.Deployment{}
			name := types.NamespacedName{
				Namespace: getNodeName(database, node),
				Name:      database.Name,
			}
			err := r.client.Get(context.TODO(), name, dep)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					return nil
				}
				return err
			}

			if dep.Status.ReadyReplicas == dep.Status.Replicas {
				partition.Status.Ready = true
				log.Info("Updating Partition status", "Name", partition.Name, "Namespace", partition.Namespace, "Ready", partition.Status.Ready)
				err = r.client.Status().Update(context.TODO(), &partition)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// getNodes returns the number of nodes in the given database
func getNodes(database *v1beta3.Database, storage *v1beta1.CacheStorageClass) int {
	partitions := database.Spec.Partitions
	if partitions == 0 {
		partitions = 1
	}
	partitionsPerNode := storage.Spec.PartitionsPerNode
	if partitionsPerNode == 0 {
		partitionsPerNode = 1
	}
	return int(math.Ceil(float64(partitions) / float64(partitionsPerNode)))
}

// getNodeForPartition returns the Node ID for the given partition ID
func getNodeForPartition(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, partition *v1beta3.Partition) int {
	return getNodeForPartitionID(database, storage, int(partition.Spec.PartitionID))
}

// getNodeForPartitionID returns the Node ID for the given partition ID
func getNodeForPartitionID(database *v1beta3.Database, storage *v1beta1.CacheStorageClass, partition int) int {
	return (partition % getNodes(database, storage)) + 1
}

// getNodeName returns the Node name
func getNodeName(database *v1beta3.Database, node int) string {
	return fmt.Sprintf("%s-%d", database.Name, node)
}

// newNodeLabels returns the labels for the given node
func newNodeLabels(database *v1beta3.Database, node int) map[string]string {
	labels := make(map[string]string)
	for key, value := range database.Labels {
		labels[key] = value
	}
	labels[nodeLabel] = fmt.Sprint(node)
	return labels
}
