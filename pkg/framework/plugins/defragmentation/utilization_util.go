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
	"sort"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"sigs.k8s.io/descheduler/pkg/api"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
)

const (
	MinResourcePercentage = 0
	MaxResourcePercentage = 100
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

func createNodeInfoSnapshot(nodes []*v1.Node, resourceName v1.ResourceName, getPodsAssignedToNode podutil.GetPodsAssignedToNodeFunc) []NodeInfo {
	var nodeInfos []NodeInfo
	for _, node := range nodes {
		pods, err := podutil.ListPodsOnANode(node.Name, getPodsAssignedToNode, nil)
		if err != nil {
			klog.V(2).Infof("Failed to obtain the pod information of the node(%s), error is %v", node.Name, err)
			continue
		}
		resourceUsage := getNodeResourceUsage(resourceName, pods)
		lastTime, _ := getNodeDefragmentationTime(node)
		nodeInfo := NodeInfo{
			node:                    node,
			resourceName:            resourceName,
			resourceUsage:           resourceUsage,
			allPods:                 pods,
			lastDefragmentationTime: lastTime,
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

func classifyNodes(
	nodeInfos []NodeInfo,
	sourceNodeFilter, targetNodeFilter func(nodeInfo NodeInfo) bool,
) (sourceNodes []NodeInfo, targetNodes []NodeInfo) {
	sourceNodes, targetNodes = []NodeInfo{}, []NodeInfo{}
	for _, nodeInfo := range nodeInfos {
		if sourceNodeFilter(nodeInfo) {
			sourceNodes = append(sourceNodes, nodeInfo)
		} else if targetNodeFilter(nodeInfo) {
			targetNodes = append(targetNodes, nodeInfo)
		}
	}
	return sourceNodes, targetNodes
}

func sortedNodesByUtilization(nodeInfos []NodeInfo, ascending bool) {
	sort.Slice(nodeInfos, func(i, j int) bool {
		ui := getNodeResourceUtilizationPercentage(nodeInfos[i], nodeInfos[i].resourceName)
		uj := getNodeResourceUtilizationPercentage(nodeInfos[j], nodeInfos[j].resourceName)
		if ascending {
			return ui < uj
		}
		return ui > uj
	})
}
