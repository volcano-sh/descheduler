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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/descheduler/pkg/api"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:deepcopy-gen=true
type DefragmentationArgs struct {
	metav1.TypeMeta `json:",inline"`

	ResourceType         v1.ResourceName        `json:"resourceType"`
	ProtectionThresholds api.ResourceThresholds `json:"protectionThresholds"`
	FragmentThresholds   api.ResourceThresholds `json:"fragmentThresholds"`
	LowThresholds        api.ResourceThresholds `json:"lowThresholds"`
	NumberOfNodes        int                    `json:"numberOfNodes"`
	CooldownTime         string                 `json:"cooldownTime"`

	//DefaultEvitorArgs
	NodeSelector            string                 `json:"nodeSelector"`
	EvictLocalStoragePods   bool                   `json:"evictLocalStoragePods"`
	EvictSystemCriticalPods bool                   `json:"evictSystemCriticalPods"`
	IgnorePvcPods           bool                   `json:"ignorePvcPods"`
	EvictFailedBarePods     bool                   `json:"evictFailedBarePods"`
	LabelSelector           *metav1.LabelSelector  `json:"labelSelector"`
	PriorityThreshold       *api.PriorityThreshold `json:"priorityThreshold"`
	NodeFit                 bool                   `json:"nodeFit"`

	// Naming this one differently since namespaces are still
	// considered while considering resources used by pods
	// but then filtered out before eviction
	EvictableNamespaces *api.Namespaces `json:"evictableNamespaces"`

	Duration string `json:"duration"`
}
