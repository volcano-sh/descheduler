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
	"fmt"
	"sort"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1helper "k8s.io/component-helpers/scheduling/corev1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/descheduler/pkg/api"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

const (
	MinResourcePercentage = 0
	MaxResourcePercentage = 100
	DefaultQueue          = "default"
)

var (
	nodeLastDefragmentationTime sync.Map
)

type NodeInfo struct {
	node                    *v1.Node
	resourceName            v1.ResourceName
	resourceUsage           resource.Quantity
	allPods                 []*v1.Pod
	lastDefragmentationTime time.Time
	allocatable             v1.ResourceList
	allocated               v1.ResourceList
}

func updateNodeDefragmentationTime(node *v1.Node) {
	now := time.Now()
	nodeLastDefragmentationTime.Store(node.Name, now)
	klog.V(4).Infof("Updated defragmentation time for node %s: %s\n", node.Name, now.Format(time.RFC3339))
}

func getNodeDefragmentationTime(node *v1.Node) (time.Time, bool) {
	value, exists := nodeLastDefragmentationTime.Load(node.Name)
	if !exists {
		return time.Time{}, false
	}
	return value.(time.Time), true
}

func clearNodeDefragmentationTime(node *v1.Node) {
	nodeLastDefragmentationTime.Delete(node.Name)
	klog.V(4).Infof("Cleared defragmentation time for node %s\n", node.Name)
}

func createNodeInfoSnapshot(nodes []*v1.Node, resourceName v1.ResourceName, getPodsAssignedToNode podutil.GetPodsAssignedToNodeFunc) []*NodeInfo {
	var nodeInfos []*NodeInfo
	for _, node := range nodes {
		pods, err := podutil.ListPodsOnANode(node.Name, getPodsAssignedToNode, nil)
		if err != nil {
			klog.V(2).Infof("Failed to obtain the pod information of the node(%s), error is %v", node.Name, err)
			continue
		}
		resourceUsage := getNodeResourceUsage(resourceName, pods)
		lastTime, _ := getNodeDefragmentationTime(node)
		nodeInfo := &NodeInfo{
			node:                    node,
			resourceName:            resourceName,
			resourceUsage:           resourceUsage,
			allPods:                 pods,
			lastDefragmentationTime: lastTime,
			allocatable:             node.Status.Allocatable,
			allocated:               getNodeAllocatedResource(pods),
		}
		nodeInfos = append(nodeInfos, nodeInfo)
	}
	return nodeInfos
}

func getNodeResourceUsage(resourceName v1.ResourceName, pods []*v1.Pod) resource.Quantity {
	var totalResourceUsage resource.Quantity
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			if request, exists := container.Resources.Requests[resourceName]; exists {
				totalResourceUsage.Add(request)
			}
		}
	}
	return totalResourceUsage
}

func getNodeResourceUtilizationPercentage(nodeInfo NodeInfo, resourceName v1.ResourceName) api.Percentage {
	allocatable, exists := nodeInfo.node.Status.Allocatable[resourceName]
	if !exists || allocatable.IsZero() {
		klog.V(1).Infof("Node %s does not have allocatable resources for %s", nodeInfo.node.Name, resourceName)
		return 0
	}

	allocated := nodeInfo.resourceUsage
	allocatedFloat := float64(allocated.MilliValue())
	allocatableFloat := float64(allocatable.MilliValue())

	return api.Percentage((allocatedFloat / allocatableFloat) * 100)
}

func getNodeAllocatedResource(pods []*v1.Pod) v1.ResourceList {
	allocated := v1.ResourceList{}
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			for resName, quantity := range container.Resources.Requests {
				if existing, ok := allocated[resName]; ok {
					existing.Add(quantity)
					allocated[resName] = existing
				} else {
					allocated[resName] = quantity.DeepCopy()
				}
			}
		}
	}
	return allocated
}

func classifyNodes(
	nodeInfos []*NodeInfo,
	sourceNodeFilter, targetNodeFilter func(nodeInfo NodeInfo) bool,
) (sourceNodes []*NodeInfo, targetNodes []*NodeInfo) {
	sourceNodes, targetNodes = []*NodeInfo{}, []*NodeInfo{}
	for _, nodeInfo := range nodeInfos {
		if sourceNodeFilter(*nodeInfo) {
			sourceNodes = append(sourceNodes, nodeInfo)
		} else if targetNodeFilter(*nodeInfo) {
			targetNodes = append(targetNodes, nodeInfo)
		}
	}
	return sourceNodes, targetNodes
}

func sortedNodesByUtilization(nodeInfos []*NodeInfo, ascending bool) {
	sort.Slice(nodeInfos, func(i, j int) bool {
		ui := getNodeResourceUtilizationPercentage(*nodeInfos[i], nodeInfos[i].resourceName)
		uj := getNodeResourceUtilizationPercentage(*nodeInfos[j], nodeInfos[j].resourceName)
		if ascending {
			return ui < uj
		}
		return ui > uj
	})
}

func classifyPods(pods []*v1.Pod, filter func(pod *v1.Pod) bool) ([]*v1.Pod, []*v1.Pod) {
	var nonRemovablePods, removablePods []*v1.Pod

	for _, pod := range pods {
		if !filter(pod) {
			nonRemovablePods = append(nonRemovablePods, pod)
		} else {
			removablePods = append(removablePods, pod)
		}
	}

	return nonRemovablePods, removablePods
}

// sorted 按指定资源Request从大到小排序Pods
func sorted(pods []*v1.Pod, resourceName v1.ResourceName) []*v1.Pod {
	sort.Slice(pods, func(i, j int) bool {
		resourceRequestI := getPodResourceRequest(pods[i], resourceName)
		resourceRequestJ := getPodResourceRequest(pods[j], resourceName)
		return resourceRequestI.Cmp(resourceRequestJ) > 0
	})
	return pods
}

func getPodResourceRequest(pod *v1.Pod, resourceName v1.ResourceName) resource.Quantity {
	var totalResourceUsage resource.Quantity
	for _, container := range pod.Spec.Containers {
		if request, exists := container.Resources.Requests[resourceName]; exists {
			totalResourceUsage.Add(request)
		}
	}
	return totalResourceUsage
}

func getPodAllResourceRequest(pod *v1.Pod) v1.ResourceList {
	podRequests := v1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		for resName, quantity := range container.Resources.Requests {
			if existing, ok := podRequests[resName]; ok {
				existing.Add(quantity)
				podRequests[resName] = existing
			} else {
				podRequests[resName] = quantity.DeepCopy()
			}
		}
	}
	return podRequests
}

func checkTaints(pod *v1.Pod, node NodeInfo) bool {
	taints := node.node.Spec.Taints
	tolerations := pod.Spec.Tolerations

	_, isUnTolerated := corev1helper.FindMatchingUntoleratedTaint(taints, tolerations, nil)
	return !isUnTolerated
}

func checkResourceAvailability(pod *v1.Pod, node NodeInfo) bool {
	podRequests := getPodAllResourceRequest(pod)
	for resName, request := range podRequests {
		allocatable := node.allocatable[resName]
		allocated := node.allocated[resName]

		available := allocatable.DeepCopy()
		available.Sub(allocated)

		if available.Cmp(request) < 0 {
			return false
		}
	}
	return true
}

// generateReservation Create a reservation object based on the pod's specifications
func generateReservation(pod *v1.Pod, targetNode *NodeInfo) *batch.Reservation {
	podSpec := pod.Spec.DeepCopy()
	// important: clear the NodeName
	podSpec.NodeName = ""
	reservation := &batch.Reservation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateReservationName(pod),
			Namespace: pod.Namespace,
		},
		Spec: batch.ReservationSpec{
			SchedulerName: pod.Spec.SchedulerName,
			MinAvailable:  1,
			Queue:         DefaultQueue,
			Owners: []batch.ReservationOwner{
				{
					Object: &v1.ObjectReference{
						Kind:      "Pod",
						Namespace: pod.Namespace,
						Name:      pod.Name,
						UID:       pod.UID,
					},
				},
			},
			Tasks: []batch.TaskSpec{
				{
					Name:     pod.Name,
					Replicas: 1,
					Template: v1.PodTemplateSpec{
						ObjectMeta: pod.ObjectMeta,
						Spec:       *podSpec,
					},
					ReservationNodeName: targetNode.node.Name,
				},
			},
			TTL: &metav1.Duration{Duration: time.Hour},
		},
	}
	if queueName, ok := pod.Annotations[batch.QueueNameKey]; ok {
		reservation.Spec.Queue = queueName
	}
	return reservation
}

func generateReservationName(pod *v1.Pod) string {
	return fmt.Sprintf("reservation-%s", pod.Name)
}

func allocateResourceToNode(node *NodeInfo, podRequests v1.ResourceList) {
	for resName, request := range podRequests {
		if allocated, exists := node.allocated[resName]; exists {
			allocated.Add(request)
			node.allocated[resName] = allocated
		} else {
			node.allocated[resName] = request.DeepCopy()
		}
	}
}

func releaseResourceFromNode(node *NodeInfo, podRequests v1.ResourceList) {
	for resName, request := range podRequests {
		if allocated, exists := node.allocated[resName]; exists {
			allocated.Sub(request)
			if allocated.IsZero() {
				delete(node.allocated, resName)
			} else {
				node.allocated[resName] = allocated
			}
		}
	}
}

func generateKeyFromPod(pod *v1.Pod) string {
	if pod == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}
