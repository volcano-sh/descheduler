/*
Copyright 2024 The Volcano Authors.

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

package defragmentation

import (
	"context"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/descheduler/pkg/descheduler/evictions"
	nodeutil "sigs.k8s.io/descheduler/pkg/descheduler/node"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
	frameworktypes "sigs.k8s.io/descheduler/pkg/framework/types"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"

	"volcano.sh/descheduler/pkg/framework/profile"
)

const (
	DefragmentationPluginName = "Defragmentation"
)

var _ frameworktypes.BalancePlugin = &Defragmentation{}

type Defragmentation struct {
	handle    frameworktypes.Handle
	args      *DefragmentationArgs
	podFilter func(pod *v1.Pod) bool
	nodeInfos []*NodeInfo
	// todo add more fields as needed
	vcClient      vcclient.Interface
	clientSet     clientset.Interface
	migratingPods sync.Map
}

// NewDefragmentation creates a new Defragmentation plugin
func NewDefragmentation(args runtime.Object, handle frameworktypes.Handle) (frameworktypes.Plugin, error) {
	defragmentationArgs, ok := args.(*DefragmentationArgs)
	if !ok {
		return nil, fmt.Errorf("want args to be of type DefragmentationArgs, got %T", args)
	}
	podFilter, err := podutil.NewOptions().
		WithFilter(handle.Evictor().Filter).
		WithLabelSelector(defragmentationArgs.LabelSelector).
		BuildFilterFunc()
	if err != nil {
		return nil, fmt.Errorf("error initializing pod filter function: %v", err)
	}

	var vcClient vcclient.Interface
	if h, ok := handle.(profile.VcHandle); ok {
		vcClient = h.VcClient()
	} else {
		return nil, fmt.Errorf("handle does not implement VcHandle")
	}

	return &Defragmentation{
		handle:        handle,
		args:          defragmentationArgs,
		podFilter:     podFilter,
		vcClient:      vcClient,
		clientSet:     handle.ClientSet(),
		migratingPods: sync.Map{},
	}, nil
}

// Name retrieves the plugin name
func (d *Defragmentation) Name() string {
	return DefragmentationPluginName
}

// Balance extension point implementation for the plugin
func (d *Defragmentation) Balance(ctx context.Context, nodes []*v1.Node) *frameworktypes.Status {
	resourceType := d.args.ResourceType
	lowThresholds := d.args.LowThresholds
	fragmentThresholds := d.args.FragmentThresholds
	protectionThresholds := d.args.ProtectionThresholds
	// 1. Acquire all nodes and calculate the resource utilization of the specified resource
	d.nodeInfos = createNodeInfoSnapshot(nodes, resourceType, d.handle.GetPodsAssignedToNodeFunc())

	var cooldownDuration time.Duration
	if d.args.CooldownTime != "" {
		duration, err := time.ParseDuration(d.args.CooldownTime)
		if err != nil {
			return &frameworktypes.Status{
				Err: fmt.Errorf("invalid cooldown time format: %s, err: %v", d.args.CooldownTime, err),
			}
		} else {
			cooldownDuration = duration
		}
	}
	var migrateTimeout time.Duration
	if d.args.ReserveTimeout != "" {
		duration, err := time.ParseDuration(d.args.ReserveTimeout)
		if err != nil {
			return &frameworktypes.Status{
				Err: fmt.Errorf("invalid ReserveTimeout format: %s, err: %v", d.args.ReserveTimeout, err),
			}
		} else {
			migrateTimeout = duration
		}
	}

	// 2. Select source and target nodes based on resource utilization
	// SourceNode Filter: resourceUtilization < lowThreshold
	sourceNodeFilter := func(nodeInfo NodeInfo) bool {
		if lastDefragmentationTime, exists := getNodeDefragmentationTime(nodeInfo.node); exists {
			if time.Since(lastDefragmentationTime) < cooldownDuration {
				return false
			}
		}
		utilization := getNodeResourceUtilizationPercentage(nodeInfo, resourceType)
		return utilization < lowThresholds[resourceType]
	}

	// TargetNode Filter：fragmentThreshold < resourceUtilization < protectionThreshold
	targetNodeFilter := func(nodeInfo NodeInfo) bool {
		if nodeutil.IsNodeUnschedulable(nodeInfo.node) {
			klog.V(2).InfoS("Node is unschedulable, thus not considered as a target node", "node", klog.KObj(nodeInfo.node))
			return false
		}
		if lastDefragmentationTime, exists := getNodeDefragmentationTime(nodeInfo.node); exists {
			if time.Since(lastDefragmentationTime) < cooldownDuration {
				return false
			}
		}
		utilization := getNodeResourceUtilizationPercentage(nodeInfo, resourceType)
		return utilization > fragmentThresholds[resourceType] && utilization < protectionThresholds[resourceType]

	}

	sourceNodes, targetNodes := classifyNodes(
		d.nodeInfos,
		sourceNodeFilter,
		targetNodeFilter,
	)

	if len(sourceNodes) == 0 {
		klog.V(1).Info("No available source nodes for migration, consider adjusting the low thresholds.")
		return nil
	}

	if len(targetNodes) == 0 {
		klog.V(1).Info("No available target nodes for migration, consider adjusting the fragmentation and protection thresholds.")
		return nil
	}

	// 3. Sort source nodes by resource utilization in ascending order, and select the first NumberOfNodes
	sortedNodesByUtilization(sourceNodes, true)
	numSourceNodesToSelect := len(sourceNodes)
	if d.args.NumberOfNodes != -1 && d.args.NumberOfNodes < len(sourceNodes) {
		numSourceNodesToSelect = d.args.NumberOfNodes
	}
	sourceNodes = sourceNodes[:numSourceNodesToSelect]

	// 4. Sort target nodes by resource utilization in descending order
	sortedNodesByUtilization(targetNodes, false)

	// todo: remove
	for i, node := range sourceNodes {
		klog.V(4).Infof("[Debug]: sourceNode[%d] = %s, allocated: %+v, allocatable: %+v", i, (*node).node.Name, (*node).allocated, (*node).allocatable)
	}
	for i, node := range targetNodes {
		klog.V(4).Infof("[Debug]: targetNode[%d] = %s, allocated: %+v, allocatable: %+v", i, (*node).node.Name, (*node).allocated, (*node).allocatable)
	}

	// 5. Start migrating pods from source nodes to target nodes
	for _, sourceNode := range sourceNodes {
		klog.V(4).Infof("[Debug]: all pods on source node %s: %d", sourceNode.node.Name, len(sourceNode.allPods))
		nonRemovablePods, removablePods := classifyPods(sourceNode.allPods, d.podFilter)
		klog.V(4).Infof("[Debug]: non-RemovablePods pods on source node %s: %d", sourceNode.node.Name, len(nonRemovablePods))
		klog.V(4).Infof("[Debug]: RemovablePods pods on source node %s: %d", sourceNode.node.Name, len(removablePods))
		pods := sorted(removablePods, d.args.ResourceType)
		klog.V(4).Infof("[Debug]: migrateable pods on source node %s: %d", sourceNode.node.Name, len(pods))
		for _, pod := range pods {
			d.migratePod(pod, sourceNode, targetNodes, migrateTimeout)
		}
	}
	return nil
}

func (d *Defragmentation) migratePod(pod *v1.Pod, sourceNode *NodeInfo, targetNodes []*NodeInfo, migrateTimeout time.Duration) {
	var targetNode *NodeInfo
	for _, node := range targetNodes {
		klog.V(4).Infof("Considering target node %s for Pod %s/%s", node.node.Name, pod.Namespace, pod.Name)
		// 1. check taints
		if !checkTaints(pod, *node) {
			klog.V(3).Infof("Pod %s/%s is not tolerating taints on node %s", pod.Namespace, pod.Name, node.node.Name)
			continue
		}

		// 2. check resource availability
		if !checkResourceAvailability(pod, *node) {
			klog.V(3).Infof("Insufficient resources on node %s for Pod %s/%s", node.node.Name, pod.Namespace, pod.Name)
			continue
		}

		targetNode = node
		break
	}

	if targetNode == nil {
		klog.Warningf("No suitable target node found for migrating Pod %s/%s from soruce node %s", pod.Namespace, pod.Name, sourceNode.node.Name)
		return
	}

	go d.doMigratePod(pod, sourceNode, targetNode, migrateTimeout)
}

func (d *Defragmentation) doMigratePod(pod *v1.Pod, sourceNode, targetNode *NodeInfo, migrateTimeout time.Duration) {
	podKey := generateKeyFromPod(pod)
	if _, exists := d.migratingPods.LoadOrStore(podKey, struct{}{}); exists {
		klog.Infof("Pod %s is already migrating, skip", podKey)
		return
	}
	defer d.migratingPods.Delete(podKey)

	klog.Infof("Starting migration of Pod %s/%s from %s to %s",
		pod.Namespace, pod.Name, sourceNode.node.Name, targetNode.node.Name)

	if !checkResourceAvailability(pod, *targetNode) {
		klog.Warningf("Insufficient resources on target node %s for Pod %s/%s", targetNode.node.Name, pod.Namespace, pod.Name)
		return
	}
	podRequests := getPodAllResourceRequest(pod)
	allocateResourceToNode(targetNode, podRequests)

	reservation := generateReservation(pod, targetNode)
	createdReservation, err := d.vcClient.BatchV1alpha1().Reservations(pod.Namespace).Create(context.TODO(), reservation, metav1.CreateOptions{})
	if err != nil {
		klog.Warningf("Failed to create reservation for Pod %s/%s: %v", pod.Namespace, pod.Name, err)
		releaseResourceFromNode(targetNode, podRequests)
		return
	}

	klog.V(4).Infof("Reservation %s created for Pod %s/%s and wait for available...", createdReservation.Name, pod.Namespace, pod.Name)

	if !d.waitForReservationAvailable(createdReservation, migrateTimeout) {
		klog.Warningf("Reservation %s for Pod %s/%s timed out", createdReservation.Name, pod.Namespace, pod.Name)
		_ = d.vcClient.BatchV1alpha1().Reservations(pod.Namespace).Delete(context.TODO(), createdReservation.Name, metav1.DeleteOptions{})
		releaseResourceFromNode(targetNode, podRequests)
		return
	}

	evictOptions := evictions.EvictOptions{
		Reason: fmt.Sprintf("Migrating Pod %s/%s from node %s to node %s", pod.Namespace, pod.Name, sourceNode.node.Name, targetNode.node.Name),
	}

	if !d.handle.Evictor().Evict(context.TODO(), pod, evictOptions) {
		klog.Errorf("Failed to evict Pod %s/%s from source node", pod.Namespace, pod.Name)
		_ = d.vcClient.BatchV1alpha1().Reservations(pod.Namespace).Delete(context.TODO(), createdReservation.Name, metav1.DeleteOptions{})
		releaseResourceFromNode(targetNode, podRequests)
		return
	}

	klog.Infof("Successfully evicted Pod %s/%s", pod.Namespace, pod.Name)
}

func (d *Defragmentation) waitForReservationAvailable(reservation *batch.Reservation, timeout time.Duration) bool {
	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return false
		case <-ticker.C:
			updated, err := d.vcClient.BatchV1alpha1().Reservations(reservation.Namespace).Get(context.TODO(), reservation.Name, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Error fetching reservation %s: %v", reservation.Name, err)
				continue
			}
			if updated.Status.State.Phase == batch.ReservationAvailable {
				klog.Infof("Reservation %s is now available", reservation.Name)
				return true
			}
		}
	}
}
