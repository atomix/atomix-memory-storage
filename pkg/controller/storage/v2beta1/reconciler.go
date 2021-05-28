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
	"context"
	"fmt"
	protocolapi "github.com/atomix/atomix-api/go/atomix/protocol"
	corev2beta1 "github.com/atomix/atomix-controller/pkg/apis/core/v2beta1"
	"k8s.io/utils/pointer"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/source"

	storagev2beta1 "github.com/atomix/atomix-memory-storage/pkg/apis/storage/v2beta1"
	"github.com/gogo/protobuf/jsonpb"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	apiPort               = 5678
	defaultImageEnv       = "DEFAULT_NODE_V2BETA1_IMAGE"
	defaultImage          = "atomix/atomix-memory-storage-node:latest"
	headlessServiceSuffix = "hs"
	appLabel              = "app"
	protocolLabel         = "protocol"
	clusterLabel          = "cluster"
	appAtomix             = "atomix"
)

const (
	configPath        = "/etc/atomix"
	clusterConfigFile = "cluster.json"
)

const (
	configVolume = "config"
)

const clusterDomainEnv = "CLUSTER_DOMAIN"

func addMemoryStoreController(mgr manager.Manager) error {
	r := &Reconciler{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}

	// Create a new controller
	c, err := controller.New(mgr.GetScheme().Name(), mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the storage resource
	err = c.Watch(&source.Kind{Type: &storagev2beta1.MemoryStore{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource StatefulSet
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		OwnerType:    &storagev2beta1.MemoryStore{},
		IsController: true,
	})
	if err != nil {
		return err
	}
	return nil
}

var _ reconcile.Reconciler = &Reconciler{}

// Reconciler reconciles a MemoryStore object
type Reconciler struct {
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Cluster object and makes changes based on the state read
// and what is in the Cluster.Spec
func (r *Reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile MemoryStore")
	protocol := &storagev2beta1.MemoryStore{}
	err := r.client.Get(context.TODO(), request.NamespacedName, protocol)
	if err != nil {
		log.Error(err, "Reconcile MemoryStore")
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	log.Info("Reconcile Clusters")
	err = r.reconcileClusters(protocol)
	if err != nil {
		log.Error(err, "Reconcile Clusters")
		return reconcile.Result{}, err
	}

	log.Info("Reconcile Protocol")
	err = r.reconcileStatus(protocol)
	if err != nil {
		log.Error(err, "Reconcile Protocol")
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *Reconciler) reconcileStatus(protocol *storagev2beta1.MemoryStore) error {
	replicas, err := r.getProtocolReplicas(protocol)
	if err != nil {
		return err
	}

	partitions, err := r.getProtocolPartitions(protocol)
	if err != nil {
		return err
	}

	if protocol.Status.ProtocolStatus == nil ||
		isReplicasChanged(protocol.Status.Replicas, replicas) ||
		isPartitionsChanged(protocol.Status.Partitions, partitions) {
		var revision int64
		if protocol.Status.ProtocolStatus != nil {
			revision = protocol.Status.ProtocolStatus.Revision
		}
		if protocol.Status.ProtocolStatus == nil ||
			!isReplicasSame(protocol.Status.Replicas, replicas) ||
			!isPartitionsSame(protocol.Status.Partitions, partitions) {
			revision++
		}
		protocol.Status.ProtocolStatus = &corev2beta1.ProtocolStatus{
			Revision:   revision,
			Replicas:   replicas,
			Partitions: partitions,
		}
		return r.client.Status().Update(context.TODO(), protocol)
	}
	return nil
}

func isReplicasSame(a, b []corev2beta1.ReplicaStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ar := a[i]
		br := b[i]
		if ar.ID != br.ID {
			return false
		}
	}
	return true
}

func isReplicasChanged(a, b []corev2beta1.ReplicaStatus) bool {
	if len(a) != len(b) {
		return true
	}
	for i := 0; i < len(a); i++ {
		ar := a[i]
		br := b[i]
		if ar.ID != br.ID {
			return true
		}
		if ar.Ready != br.Ready {
			return true
		}
	}
	return false
}

func isPartitionsSame(a, b []corev2beta1.PartitionStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ap := a[i]
		bp := b[i]
		if ap.ID != bp.ID {
			return false
		}
		if len(ap.Replicas) != len(bp.Replicas) {
			return false
		}
		for j := 0; j < len(ap.Replicas); j++ {
			if ap.Replicas[j] != bp.Replicas[j] {
				return false
			}
		}
	}
	return true
}

func isPartitionsChanged(a, b []corev2beta1.PartitionStatus) bool {
	if len(a) != len(b) {
		return true
	}
	for i := 0; i < len(a); i++ {
		ap := a[i]
		bp := b[i]
		if ap.ID != bp.ID {
			return true
		}
		if len(ap.Replicas) != len(bp.Replicas) {
			return true
		}
		for j := 0; j < len(ap.Replicas); j++ {
			if ap.Replicas[j] != bp.Replicas[j] {
				return true
			}
		}
		if ap.Ready != bp.Ready {
			return true
		}
	}
	return false
}

func (r *Reconciler) getProtocolReplicas(protocol *storagev2beta1.MemoryStore) ([]corev2beta1.ReplicaStatus, error) {
	numClusters := getClusters(protocol)
	replicas := make([]corev2beta1.ReplicaStatus, 0, numClusters)
	for i := 1; i <= numClusters; i++ {
		replicaReady, err := r.isClusterReady(protocol, i)
		if err != nil {
			return nil, err
		}
		replica := corev2beta1.ReplicaStatus{
			ID:    getClusterName(protocol, i),
			Host:  pointer.StringPtr(getClusterDNSName(protocol, i)),
			Port:  pointer.Int32Ptr(int32(apiPort)),
			Ready: replicaReady,
		}
		replicas = append(replicas, replica)
	}
	return replicas, nil
}

func (r *Reconciler) getProtocolPartitions(protocol *storagev2beta1.MemoryStore) ([]corev2beta1.PartitionStatus, error) {
	numClusters := getClusters(protocol)
	partitions := make([]corev2beta1.PartitionStatus, 0, protocol.Spec.Partitions)
	for partitionID := 1; partitionID <= int(protocol.Spec.Partitions); partitionID++ {
		for i := 1; i <= numClusters; i++ {
			partitionReady, err := r.isClusterReady(protocol, i)
			if err != nil {
				return nil, err
			}
			partition := corev2beta1.PartitionStatus{
				ID:       uint32(partitionID),
				Replicas: []string{getClusterName(protocol, i)},
				Ready:    partitionReady,
			}
			partitions = append(partitions, partition)
		}
	}
	return partitions, nil
}

func (r *Reconciler) isClusterReady(protocol *storagev2beta1.MemoryStore, cluster int) (bool, error) {
	depName := types.NamespacedName{
		Namespace: protocol.Namespace,
		Name:      getClusterName(protocol, cluster),
	}
	dep := &appsv1.Deployment{}
	if err := r.client.Get(context.TODO(), depName, dep); err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return false, err
	}

	pods := &corev1.PodList{}
	if err := r.client.List(context.TODO(), pods, &client.ListOptions{LabelSelector: selector}); err != nil {
		return false, err
	}

	for _, pod := range pods.Items {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *Reconciler) reconcileClusters(protocol *storagev2beta1.MemoryStore) error {
	clusters := getClusters(protocol)
	for cluster := 1; cluster <= clusters; cluster++ {
		err := r.reconcileCluster(protocol, cluster)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcileCluster(protocol *storagev2beta1.MemoryStore, cluster int) error {
	err := r.reconcileConfigMap(protocol, cluster)
	if err != nil {
		return err
	}

	err = r.reconcileDeployment(protocol, cluster)
	if err != nil {
		return err
	}

	err = r.reconcileService(protocol, cluster)
	if err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) reconcileConfigMap(protocol *storagev2beta1.MemoryStore, cluster int) error {
	log.Info("Reconcile memory protocol config map")
	cm := &corev1.ConfigMap{}
	name := types.NamespacedName{
		Namespace: protocol.Namespace,
		Name:      getClusterName(protocol, cluster),
	}
	err := r.client.Get(context.TODO(), name, cm)
	if err != nil && k8serrors.IsNotFound(err) {
		err = r.addConfigMap(protocol, cluster)
	}
	return err
}

func (r *Reconciler) addConfigMap(protocol *storagev2beta1.MemoryStore, cluster int) error {
	log.Info("Creating memory ConfigMap", "Name", protocol.Name, "Namespace", protocol.Namespace)
	clusterConfig, err := newNodeConfigString(protocol, cluster)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getClusterName(protocol, cluster),
			Namespace: protocol.Namespace,
			Labels:    newClusterLabels(protocol, cluster),
		},
		Data: map[string]string{
			clusterConfigFile: clusterConfig,
		},
	}

	if err := controllerutil.SetControllerReference(protocol, cm, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), cm)
}

// newNodeConfigString creates a node configuration string for the given cluster
func newNodeConfigString(protocol *storagev2beta1.MemoryStore, cluster int) (string, error) {
	replicas := []protocolapi.ProtocolReplica{
		{
			ID:      getClusterName(protocol, cluster),
			Host:    fmt.Sprintf("%s.%s.svc.cluster.local", getClusterName(protocol, cluster), protocol.Namespace),
			APIPort: apiPort,
		},
	}

	partitions := make([]protocolapi.ProtocolPartition, 0, protocol.Spec.Partitions)
	for partitionID := 1; partitionID <= int(protocol.Spec.Partitions); partitionID++ {
		if getClusterForPartitionID(protocol, partitionID) == cluster {
			partition := protocolapi.ProtocolPartition{
				PartitionID: uint32(partitionID),
				Replicas:    []string{getClusterName(protocol, cluster)},
			}
			partitions = append(partitions, partition)
		}
	}

	config := &protocolapi.ProtocolConfig{
		Replicas:   replicas,
		Partitions: partitions,
	}

	marshaller := jsonpb.Marshaler{}
	return marshaller.MarshalToString(config)
}

func (r *Reconciler) reconcileDeployment(protocol *storagev2beta1.MemoryStore, cluster int) error {
	log.Info("Reconcile memory protocol stateful set")
	statefulSet := &appsv1.StatefulSet{}
	name := types.NamespacedName{
		Namespace: protocol.Namespace,
		Name:      getClusterName(protocol, cluster),
	}
	err := r.client.Get(context.TODO(), name, statefulSet)
	if err != nil && k8serrors.IsNotFound(err) {
		err = r.addDeployment(protocol, cluster)
	}
	return err
}

func (r *Reconciler) addDeployment(protocol *storagev2beta1.MemoryStore, cluster int) error {
	log.Info("Creating Deployment", "Name", getClusterName(protocol, cluster), "Namespace", protocol.Namespace)
	var replicas int32 = 1
	image := getImage(protocol)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: protocol.Namespace,
			Name:      getClusterName(protocol, cluster),
			Labels:    newClusterLabels(protocol, cluster),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: newClusterLabels(protocol, cluster),
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      getClusterName(protocol, cluster),
					Namespace: protocol.Namespace,
					Labels:    newClusterLabels(protocol, cluster),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "memory",
							Image:           image,
							ImagePullPolicy: protocol.Spec.ImagePullPolicy,
							Args: []string{
								"$(NODE_ID)",
								fmt.Sprintf("%s/%s", configPath, clusterConfigFile),
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
										Name: getClusterName(protocol, cluster),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(protocol, dep, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), dep)
}

func (r *Reconciler) reconcileService(protocol *storagev2beta1.MemoryStore, cluster int) error {
	log.Info("Reconcile memory protocol headless service")
	service := &corev1.Service{}
	name := types.NamespacedName{
		Namespace: protocol.Namespace,
		Name:      getClusterHeadlessServiceName(protocol, cluster),
	}
	err := r.client.Get(context.TODO(), name, service)
	if err != nil && k8serrors.IsNotFound(err) {
		err = r.addService(protocol, cluster)
	}
	return err
}

func (r *Reconciler) addService(protocol *storagev2beta1.MemoryStore, cluster int) error {
	log.Info("Creating headless memory service", "Name", protocol.Name, "Namespace", protocol.Namespace)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getClusterHeadlessServiceName(protocol, cluster),
			Namespace: protocol.Namespace,
			Labels:    newClusterLabels(protocol, cluster),
			Annotations: map[string]string{
				"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
			},
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
			Selector:                 newClusterLabels(protocol, cluster),
		},
	}

	if err := controllerutil.SetControllerReference(protocol, service, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), service)
}

// getClusters returns the number of clusters in the given database
func getClusters(storage *storagev2beta1.MemoryStore) int {
	if storage.Spec.Clusters == 0 {
		return 1
	}
	return int(storage.Spec.Clusters)
}

// getClusterForPartitionID returns the cluster ID for the given partition ID
func getClusterForPartitionID(protocol *storagev2beta1.MemoryStore, partition int) int {
	return (partition % getClusters(protocol)) + 1
}

// getClusterResourceName returns the given resource name for the given cluster
func getClusterResourceName(protocol *storagev2beta1.MemoryStore, cluster int, resource string) string {
	return fmt.Sprintf("%s-%s", getClusterName(protocol, cluster), resource)
}

// getClusterName returns the cluster name
func getClusterName(protocol *storagev2beta1.MemoryStore, cluster int) string {
	return fmt.Sprintf("%s-%d", protocol.Name, cluster)
}

// getClusterHeadlessServiceName returns the headless service name for the given cluster
func getClusterHeadlessServiceName(protocol *storagev2beta1.MemoryStore, cluster int) string {
	return getClusterResourceName(protocol, cluster, headlessServiceSuffix)
}

// getClusterDNSName returns the fully qualified DNS name for the given pod ID
func getClusterDNSName(protocol *storagev2beta1.MemoryStore, cluster int) string {
	domain := os.Getenv(clusterDomainEnv)
	if domain == "" {
		domain = "cluster.local"
	}
	return fmt.Sprintf("%s.%s.%s.svc.%s", getClusterName(protocol, cluster), getClusterHeadlessServiceName(protocol, cluster), protocol.Namespace, domain)
}

// newClusterLabels returns the labels for the given cluster
func newClusterLabels(protocol *storagev2beta1.MemoryStore, cluster int) map[string]string {
	labels := make(map[string]string)
	for key, value := range protocol.Labels {
		labels[key] = value
	}
	labels[appLabel] = appAtomix
	labels[protocolLabel] = fmt.Sprintf("%s.%s", protocol.Name, protocol.Namespace)
	labels[clusterLabel] = fmt.Sprint(cluster)
	return labels
}

func getImage(protocol *storagev2beta1.MemoryStore) string {
	if protocol.Spec.Image != "" {
		return protocol.Spec.Image
	}
	return getDefaultImage()
}

func getDefaultImage() string {
	image := os.Getenv(defaultImageEnv)
	if image == "" {
		image = defaultImage
	}
	return image
}
