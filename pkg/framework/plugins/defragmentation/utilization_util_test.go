package defragmentation

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	extendedResource = v1.ResourceName("nvidia.com/gpu")
)

func fakePod(requests v1.ResourceList) *v1.Pod {
	return &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: requests,
					},
				},
			},
		},
	}
}

func fakeNode(name string, allocatable v1.ResourceList) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: v1.NodeStatus{
			Allocatable: allocatable,
		},
	}
}

func fakeNodeInfo() NodeInfo {
	request := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("50m"),
		v1.ResourceMemory: resource.MustParse("100Mi"),
		extendedResource:  resource.MustParse("2"),
	}
	allocatable := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("100m"),
		v1.ResourceMemory: resource.MustParse("200Mi"),
		extendedResource:  resource.MustParse("4"),
	}
	return NodeInfo{
		node:                    fakeNode("node1", allocatable),
		resourceName:            v1.ResourceCPU,
		resourceUsage:           resource.MustParse("50m"),
		allPods:                 []*v1.Pod{fakePod(request)},
		lastDefragmentationTime: time.Now(),
	}
}

func TestGetNodeResourceUtilizationPercentage(t *testing.T) {
	nodeInfo := fakeNodeInfo()
	usagePercentage := getNodeResourceUtilizationPercentage(nodeInfo, v1.ResourceCPU)
	if usagePercentage != 50 {
		t.Errorf("Incorrect percentange computation, expected %v, got math.Floor(%v) instead", 50, usagePercentage)
	}
}

func TestGetNodeResourceUsage(t *testing.T) {
	tests := []struct {
		resourceName v1.ResourceName
		expected     string // 期望的资源值，使用字符串以便转换
	}{
		{v1.ResourceCPU, "300m"},
		{v1.ResourceMemory, "600Mi"},
		{extendedResource, "12"},
	}
	request1 := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("100m"),
		v1.ResourceMemory: resource.MustParse("200Mi"),
		extendedResource:  resource.MustParse("4"),
	}
	request2 := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("200m"),
		v1.ResourceMemory: resource.MustParse("400Mi"),
		extendedResource:  resource.MustParse("8"),
	}
	pods := []*v1.Pod{
		fakePod(request1),
		fakePod(request2),
	}

	for _, test := range tests {
		resourceUsage := getNodeResourceUsage(test.resourceName, pods)
		if resourceUsage.String() != test.expected {
			t.Errorf("Expected %s, but got %s for resource %s", test.expected, resourceUsage.String(), test.resourceName)
		}
	}
}

func TestDefragmentationTime(t *testing.T) {
	node := fakeNode("node1", nil)
	updateNodeDefragmentationTime(node)
	_, exists := getNodeDefragmentationTime(node)
	if !exists {
		t.Errorf("Expected defragmentation time to exist for node %s", node.Name)
	}
}

func TestClassifyNodes(t *testing.T) {
	nodeInfos := []NodeInfo{fakeNodeInfo()}
	sourceNodes, targetNodes := classifyNodes(nodeInfos, func(nodeInfo NodeInfo) bool { return true }, func(nodeInfo NodeInfo) bool { return false })
	if len(sourceNodes) != 1 {
		t.Errorf("Expected 1 source nodes, but got %d", len(sourceNodes))
	}
	if len(targetNodes) != 0 {
		t.Errorf("Expected 0 target nodes, but got %d", len(targetNodes))
	}
}

func TestSortedNodesByUtilization(t *testing.T) {
	nodeInfos := []NodeInfo{
		fakeNodeInfo(), fakeNodeInfo(),
	}
	nodeInfos[0].resourceUsage = resource.MustParse("100m")

	sortedNodesByUtilization(nodeInfos, true)
	if nodeInfos[0].resourceUsage.Cmp(nodeInfos[1].resourceUsage) != -1 {
		t.Errorf("Expected nodes to be sorted in ascending order")
	}

	sortedNodesByUtilization(nodeInfos, false)
	if nodeInfos[0].resourceUsage.Cmp(nodeInfos[1].resourceUsage) != 1 {
		t.Errorf("Expected nodes to be sorted in descending order")
	}
}
