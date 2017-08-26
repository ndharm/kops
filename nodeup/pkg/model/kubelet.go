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

package model

import (
	"fmt"
	"path/filepath"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/kops/nodeup/pkg/distros"
	"k8s.io/kops/pkg/apis/kops"
	"k8s.io/kops/pkg/apis/kops/util"
	"k8s.io/kops/pkg/flagbuilder"
	"k8s.io/kops/pkg/systemd"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kops/upup/pkg/fi/nodeup/nodetasks"
	"k8s.io/kops/upup/pkg/fi/utils"

	"github.com/blang/semver"
	"github.com/golang/glog"
)

// KubeletBuilder install kubelet
type KubeletBuilder struct {
	*NodeupModelContext
}

var _ fi.ModelBuilder = &KubeletBuilder{}

// Build is responsible for building the kubelet configuration
func (b *KubeletBuilder) Build(c *fi.ModelBuilderContext) error {
	kubeletConfig, err := b.buildKubeletConfig()
	if err != nil {
		return fmt.Errorf("error building kubelet config: %v", err)
	}

	{
		t, err := b.buildSystemdEnvironmentFile(kubeletConfig)
		if err != nil {
			return err
		}
		c.AddTask(t)
	}

	{
		// @TODO Extract to common function?
		assetName := "kubelet"
		assetPath := ""
		// @TODO make Find call to an interface, we cannot mock out this function because it finds a file on disk
		asset, err := b.Assets.Find(assetName, assetPath)
		if err != nil {
			return fmt.Errorf("error trying to locate asset %q: %v", assetName, err)
		}
		if asset == nil {
			return fmt.Errorf("unable to locate asset %q", assetName)
		}

		t := &nodetasks.File{
			Path:     b.kubeletPath(),
			Contents: asset,
			Type:     nodetasks.FileType_File,
			Mode:     s("0755"),
		}
		c.AddTask(t)
	}

	{
		// @TODO Change kubeconfig to be https
		kubeconfig, err := b.buildPKIKubeconfig("kubelet")
		if err != nil {
			return err
		}
		t := &nodetasks.File{
			Path:     "/var/lib/kubelet/kubeconfig",
			Contents: fi.NewStringResource(kubeconfig),
			Type:     nodetasks.FileType_File,
			Mode:     s("0400"),
		}
		c.AddTask(t)
	}

	if b.UsesCNI() {
		t := &nodetasks.File{
			Path: b.CNIConfDir(),
			Type: nodetasks.FileType_Directory,
		}
		c.AddTask(t)
	}

	if err := b.addStaticUtils(c); err != nil {
		return err
	}

	c.AddTask(b.buildSystemdService())

	return nil
}

// kubeletPath returns the path of the kubelet based on distro
func (b *KubeletBuilder) kubeletPath() string {
	kubeletCommand := "/usr/local/bin/kubelet"
	if b.Distribution == distros.DistributionCoreOS {
		kubeletCommand = "/opt/kubernetes/bin/kubelet"
	}
	if b.Distribution == distros.DistributionContainerOS {
		kubeletCommand = "/home/kubernetes/bin/kubelet"
	}
	return kubeletCommand
}

// buildSystemdEnvironmentFile renders the environment file for the kubelet
func (b *KubeletBuilder) buildSystemdEnvironmentFile(kubeletConfig *kops.KubeletConfigSpec) (*nodetasks.File, error) {
	// TODO: Dump the separate file for flags - just complexity!
	flags, err := flagbuilder.BuildFlags(kubeletConfig)
	if err != nil {
		return nil, fmt.Errorf("error building kubelet flags: %v", err)
	}

	// Add cloud config file if needed
	// We build this flag differently because it depends on CloudConfig, and to expose it directly
	// would be a degree of freedom we don't have (we'd have to write the config to different files)
	// We can always add this later if it is needed.
	if b.Cluster.Spec.CloudConfig != nil {
		flags += " --cloud-config=" + CloudConfigFilePath
	}

	if b.UsesCNI() {
		flags += " --cni-bin-dir=" + b.CNIBinDir()
		flags += " --cni-conf-dir=" + b.CNIConfDir()
	}

	if b.Cluster.Spec.Networking != nil && b.Cluster.Spec.Networking.Kubenet != nil {
		// Kubenet is neither CNI nor not-CNI, so we need to pass it `--network-plugin-dir` also
		flags += " --network-plugin-dir=" + b.CNIBinDir()
	}

	sysconfig := "DAEMON_ARGS=\"" + flags + "\"\n"
	// Makes kubelet read /root/.docker/config.json properly
	sysconfig = sysconfig + "HOME=\"/root" + "\"\n"

	t := &nodetasks.File{
		Path:     "/etc/sysconfig/kubelet",
		Contents: fi.NewStringResource(sysconfig),
		Type:     nodetasks.FileType_File,
	}
	return t, nil
}

// buildSystemdService is responsible for generating the kubelet systemd unit
func (b *KubeletBuilder) buildSystemdService() *nodetasks.Service {
	kubeletCommand := b.kubeletPath()

	manifest := &systemd.Manifest{}
	manifest.Set("Unit", "Description", "Kubernetes Kubelet Server")
	manifest.Set("Unit", "Documentation", "https://github.com/kubernetes/kubernetes")
	manifest.Set("Unit", "After", "docker.service")

	if b.Distribution == distros.DistributionCoreOS {
		// We add /opt/kubernetes/bin for our utilities (socat)
		manifest.Set("Service", "Environment", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/kubernetes/bin")
	}

	manifest.Set("Service", "EnvironmentFile", "/etc/sysconfig/kubelet")
	manifest.Set("Service", "ExecStart", kubeletCommand+" \"$DAEMON_ARGS\"")
	manifest.Set("Service", "Restart", "always")
	manifest.Set("Service", "RestartSec", "2s")
	manifest.Set("Service", "StartLimitInterval", "0")
	manifest.Set("Service", "KillMode", "process")
	manifestString := manifest.Render()

	glog.V(8).Infof("Built service manifest %q\n%s", "kubelet", manifestString)

	service := &nodetasks.Service{
		Name:       "kubelet.service",
		Definition: s(manifestString),
	}

	// @check if we are a master allow protokube to start kubelet
	if b.IsMaster {
		service.Running = fi.Bool(false)
	}

	service.InitDefaults()

	return service
}

func (b *KubeletBuilder) buildKubeletConfig() (*kops.KubeletConfigSpec, error) {
	if b.InstanceGroup == nil {
		glog.Fatalf("InstanceGroup was not set")
	}
	kubeletConfigSpec, err := b.buildKubeletConfigSpec()
	if err != nil {
		return nil, fmt.Errorf("error building kubelet config: %v", err)
	}

	// TODO: Memoize if we reuse this
	return kubeletConfigSpec, nil
}

func (b *KubeletBuilder) addStaticUtils(c *fi.ModelBuilderContext) error {
	if b.Distribution == distros.DistributionCoreOS {
		// CoreOS does not ship with socat.  Install our own (statically linked) version
		// TODO: Extract to common function?
		assetName := "socat"
		assetPath := ""
		asset, err := b.Assets.Find(assetName, assetPath)
		if err != nil {
			return fmt.Errorf("error trying to locate asset %q: %v", assetName, err)
		}
		if asset == nil {
			return fmt.Errorf("unable to locate asset %q", assetName)
		}

		t := &nodetasks.File{
			Path:     "/opt/kubernetes/bin/socat",
			Contents: asset,
			Type:     nodetasks.FileType_File,
			Mode:     s("0755"),
		}
		c.AddTask(t)
	}

	return nil
}

const RoleLabelName15 = "kubernetes.io/role"
const RoleLabelName16 = "kubernetes.io/role"
const RoleMasterLabelValue15 = "master"
const RoleNodeLabelValue15 = "node"

const RoleLabelMaster16 = "node-role.kubernetes.io/master"
const RoleLabelNode16 = "node-role.kubernetes.io/node"

// NodeLabels are defined in the InstanceGroup, but set flags on the kubelet config.
// We have a conflict here: on the one hand we want an easy to use abstract specification
// for the cluster, on the other hand we don't want two fields that do the same thing.
// So we make the logic for combining a KubeletConfig part of our core logic.
// NodeLabels are set on the instanceGroup.  We might allow specification of them on the kubelet
// config as well, but for now the precedence is not fully specified.
// (Today, NodeLabels on the InstanceGroup are merged in to NodeLabels on the KubeletConfig in the Cluster).
// In future, we will likely deprecate KubeletConfig in the Cluster, and move it into componentconfig,
// once that is part of core k8s.

// buildKubeletConfigSpec returns the kubeletconfig for the specified instanceGroup
func (b *KubeletBuilder) buildKubeletConfigSpec() (*kops.KubeletConfigSpec, error) {
	sv, err := util.ParseKubernetesVersion(b.Cluster.Spec.KubernetesVersion)
	if err != nil {
		return nil, fmt.Errorf("Failed to lookup kubernetes version: %v", err)
	}

	// Merge KubeletConfig for NodeLabels
	c := &kops.KubeletConfigSpec{}
	if b.InstanceGroup.Spec.Role == kops.InstanceGroupRoleMaster {
		utils.JsonMergeStruct(c, b.Cluster.Spec.MasterKubelet)
	} else {
		utils.JsonMergeStruct(c, b.Cluster.Spec.Kubelet)
	}

	// @check if we are using secure kubelet <-> api settings
	if b.UseSecureKubelet() {
		// @TODO these filenames need to be a constant somewhere
		c.ClientCAFile = filepath.Join(b.PathSrvKubernetes(), "ca.crt")
	}

	if b.InstanceGroup.Spec.Kubelet != nil {
		utils.JsonMergeStruct(c, b.InstanceGroup.Spec.Kubelet)
	}

	if b.InstanceGroup.Spec.Role == kops.InstanceGroupRoleMaster {
		if c.NodeLabels == nil {
			c.NodeLabels = make(map[string]string)
		}
		c.NodeLabels[RoleLabelMaster16] = ""
		c.NodeLabels[RoleLabelName15] = RoleMasterLabelValue15
	} else {
		if c.NodeLabels == nil {
			c.NodeLabels = make(map[string]string)
		}
		c.NodeLabels[RoleLabelNode16] = ""
		c.NodeLabels[RoleLabelName15] = RoleNodeLabelValue15
	}

	for k, v := range b.InstanceGroup.Spec.NodeLabels {
		if c.NodeLabels == nil {
			c.NodeLabels = make(map[string]string)
		}
		c.NodeLabels[k] = v
	}

	// --register-with-taints was available in the first 1.6.0 alpha, no need to rely on semver's pre/build ordering
	sv.Pre = nil
	sv.Build = nil
	if sv.GTE(semver.Version{Major: 1, Minor: 6, Patch: 0, Pre: nil, Build: nil}) {
		for _, t := range b.InstanceGroup.Spec.Taints {
			c.Taints = append(c.Taints, t)
		}

		if len(c.Taints) == 0 && b.IsMaster {
			// (Even though the value is empty, we still expect <Key>=<Value>:<Effect>)
			c.Taints = append(c.Taints, RoleLabelMaster16+"=:"+string(v1.TaintEffectNoSchedule))
		}

		// Enable scheduling since it can be controlled via taints.
		// For pre-1.6.0 clusters, this is handled by tainter.go
		c.RegisterSchedulable = fi.Bool(true)
	} else {
		// For 1.5 and earlier, protokube will taint the master
	}

	return c, nil
}
