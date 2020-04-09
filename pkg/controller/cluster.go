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

package test

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	api "github.com/atomix/api/proto/atomix/controller"
	"github.com/atomix/cache-storage/pkg/apis/storage/v1beta1"
	"github.com/atomix/kubernetes-controller/pkg/apis/cloud/v1beta2"
	"github.com/golang/protobuf/jsonpb"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *Reconciler) addService(cluster *v1beta2.Cluster, storage *v1beta1.CacheStorageClass) error {
	log.Info("Creating service", "Name", cluster.Name, "Namespace", cluster.Namespace)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
			Labels:    cluster.Labels,
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
			Selector:                 cluster.Labels,
		},
	}
	if err := controllerutil.SetControllerReference(cluster, service, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), service)
}

func (r *Reconciler) addDeployment(cluster *v1beta2.Cluster, storage *v1beta1.CacheStorageClass) error {
	log.Info("Creating Deployment", "Name", cluster.Name, "Namespace", cluster.Namespace)
	var replicas int32 = 1
	var env []corev1.EnvVar
	env = append(env, corev1.EnvVar{
		Name: "NODE_ID",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.name",
			},
		},
	})
	args := []string{
		"$(NODE_ID)",
		fmt.Sprintf("%s/%s", configPath, clusterConfigFile),
		fmt.Sprintf("%s/%s", configPath, protocolConfigFile),
	}

	volumes := []corev1.Volume{
		newConfigVolume(cluster),
	}

	volumeMounts := []corev1.VolumeMount{
		newConfigVolumeMount(),
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
			Labels:    cluster.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cluster.Labels,
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
					Labels:    cluster.Labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            storage.Name,
							Image:           storage.Spec.Image,
							ImagePullPolicy: storage.Spec.ImagePullPolicy,
							Args:            args,
							Env:             env,
							VolumeMounts:    volumeMounts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, dep, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), dep)
}

func (r *Reconciler) addConfigMap(cluster *v1beta2.Cluster, storage *v1beta1.CacheStorageClass) error {
	log.Info("Creating ConfigMap", "Name", cluster.Name, "Namespace", cluster.Namespace)
	config, err := newClusterConfig(cluster)
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
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
			Labels:    cluster.Labels,
		},
		Data: map[string]string{
			clusterConfigFile: data,
		},
	}
	if err := controllerutil.SetControllerReference(cluster, cm, r.scheme); err != nil {
		return err
	}
	return r.client.Create(context.TODO(), cm)
}

// newNodeConfigString creates a node configuration string for the given cluster
func newClusterConfig(cluster *v1beta2.Cluster) (*api.ClusterConfig, error) {
	database := cluster.Annotations["cloud.atomix.io/database"]
	clusterIDstr, ok := cluster.Annotations["cloud.atomix.io/cluster"]
	if !ok {
		return nil, errors.New("missing cluster annotation")
	}

	id, err := strconv.ParseInt(clusterIDstr, 0, 32)
	if err != nil {
		return nil, err
	}
	clusterID := int32(id)

	members := []*api.MemberConfig{
		{
			ID:           cluster.Name,
			Host:         fmt.Sprintf("%s.%s.svc.cluster.local", cluster.Name, cluster.Namespace),
			ProtocolPort: apiPort,
			APIPort:      apiPort,
		},
	}

	partitions := make([]*api.PartitionId, 0, cluster.Spec.Partitions)
	for partitionID := (cluster.Spec.Partitions * (clusterID - 1)) + 1; partitionID <= cluster.Spec.Partitions*clusterID; partitionID++ {
		partition := &api.PartitionId{
			Partition: partitionID,
			Cluster: &api.ClusterId{
				ID: int32(clusterID),
				DatabaseID: &api.DatabaseId{
					Name:      database,
					Namespace: cluster.Namespace,
				},
			},
		}
		partitions = append(partitions, partition)
	}

	return &api.ClusterConfig{
		Members:    members,
		Partitions: partitions,
	}, nil
}

// newConfigVolumeMount returns a configuration volume mount for a pod
func newConfigVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      configVolume,
		MountPath: configPath,
	}
}

// newConfigVolume returns the configuration volume for a pod
func newConfigVolume(cluster *v1beta2.Cluster) corev1.Volume {
	return corev1.Volume{
		Name: configVolume,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cluster.Name,
				},
			},
		},
	}
}
