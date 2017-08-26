/*
Copyright 2016 The Kubernetes Authors.

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

package vfsclientset

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kops/pkg/apis/kops"
	kopsinternalversion "k8s.io/kops/pkg/client/clientset_generated/clientset/typed/kops/internalversion"
	"k8s.io/kops/pkg/client/simple"
	"k8s.io/kops/util/pkg/vfs"
)

type VFSClientset struct {
	basePath vfs.Path
}

var _ simple.Clientset = &VFSClientset{}

func (c *VFSClientset) clusters() *ClusterVFS {
	return newClusterVFS(c.basePath)
}

// GetCluster implements the GetCluster method of simple.Clientset for a VFS-backed state store
func (c *VFSClientset) GetCluster(name string) (*kops.Cluster, error) {
	return c.clusters().Get(name, metav1.GetOptions{})
}

// UpdateCluster implements the UpdateCluster method of simple.Clientset for a VFS-backed state store
func (c *VFSClientset) UpdateCluster(cluster *kops.Cluster) (*kops.Cluster, error) {
	return c.clusters().Update(cluster)
}

// CreateCluster implements the CreateCluster method of simple.Clientset for a VFS-backed state store
func (c *VFSClientset) CreateCluster(cluster *kops.Cluster) (*kops.Cluster, error) {
	return c.clusters().Create(cluster)
}

// ListClusters implements the ListClusters method of simple.Clientset for a VFS-backed state store
func (c *VFSClientset) ListClusters(options metav1.ListOptions) (*kops.ClusterList, error) {
	return c.clusters().List(options)
}

// ConfigBaseFor implements the ConfigBaseFor method of simple.Clientset for a VFS-backed state store
func (c *VFSClientset) ConfigBaseFor(cluster *kops.Cluster) (vfs.Path, error) {
	return c.clusters().configBase(cluster.Name)
}

// InstanceGroupsFor implements the InstanceGroupsFor method of simple.Clientset for a VFS-backed state store
func (c *VFSClientset) InstanceGroupsFor(cluster *kops.Cluster) kopsinternalversion.InstanceGroupInterface {
	clusterName := cluster.Name
	return newInstanceGroupVFS(c, clusterName)
}

func (c *VFSClientset) federations() kopsinternalversion.FederationInterface {
	return newFederationVFS(c)
}

// FederationsFor implements the FederationsFor method of simple.Clientset for a VFS-backed state store
func (c *VFSClientset) FederationsFor(federation *kops.Federation) kopsinternalversion.FederationInterface {
	return c.federations()
}

// ListFederations implements the ListFederations method of simple.Clientset for a VFS-backed state store
func (c *VFSClientset) ListFederations(options metav1.ListOptions) (*kops.FederationList, error) {
	return c.federations().List(options)
}

// GetFederation implements the GetFederation method of simple.Clientset for a VFS-backed state store
func (c *VFSClientset) GetFederation(name string) (*kops.Federation, error) {
	return c.federations().Get(name, metav1.GetOptions{})
}

func NewVFSClientset(basePath vfs.Path) simple.Clientset {
	vfsClientset := &VFSClientset{
		basePath: basePath,
	}
	return vfsClientset
}
