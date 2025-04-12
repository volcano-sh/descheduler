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
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	nodeutil "sigs.k8s.io/descheduler/pkg/descheduler/node"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
	frameworktypes "sigs.k8s.io/descheduler/pkg/framework/types"
)

const (
	DefragmentationPluginName = "Defragmentation"
)

var _ frameworktypes.BalancePlugin = &Defragmentation{}

type Defragmentation struct {
	handle    frameworktypes.Handle
	args      *DefragmentationArgs
	podFilter func(pod *v1.Pod) bool
	nodeInfos []NodeInfo
	// todo add more fields as needed
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
	// Todo: add more fields as needed
	return &Defragmentation{
		handle:    handle,
		args:      defragmentationArgs,
		podFilter: podFilter,
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
				Err: fmt.Errorf("Invalid cooldown time format: %s, err: %v", d.args.CooldownTime, err),
			}
		} else {
			cooldownDuration = duration
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

	klog.V(4).Infof("[Debug]: sourceNodes = %+v", sourceNodes)
	klog.V(4).Infof("[Debug]: targetNodes = %+v", targetNodes)

	// TODO
	// 5. Generate migration plan
	return nil
}
