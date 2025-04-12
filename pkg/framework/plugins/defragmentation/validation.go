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

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/descheduler/pkg/api"
)

// ValidateDefragmentationArgs validates the DefragmentationArgs object.
func ValidateDefragmentationArgs(obj runtime.Object) error {
	args, ok := obj.(*DefragmentationArgs)
	if !ok {
		klog.Errorf("obj with type %T could not parse", obj)
		return fmt.Errorf("obj with type %T could not parse", obj)
	}
	// Validate ResourceType
	if err := validateResourceType(args.ResourceType); err != nil {
		return err
	}

	// Validate Thresholds
	if err := validateThresholds(args.ProtectionThresholds, "ProtectionThresholds"); err != nil {
		return err
	}
	if err := validateThresholds(args.FragmentThresholds, "FragmentThresholds"); err != nil {
		return err
	}
	if err := validateThresholds(args.LowThresholds, "LowThresholds"); err != nil {
		return err
	}
	if err := validateDefragmentationThresholds(args.LowThresholds, args.FragmentThresholds, args.ProtectionThresholds); err != nil {
		return err
	}

	// Validate EvictableNamespaces
	if err := validateEvictableNamespaces(args.EvictableNamespaces); err != nil {
		return err
	}

	return nil
}

// validateResourceType checks if the resource type is valid.
func validateResourceType(resourceType v1.ResourceName) error {
	if len(resourceType) == 0 || resourceType == "" {
		return fmt.Errorf("ResourceType must not be empty")
	}
	return nil
}

// validateThresholds checks if thresholds have valid resource name and resource percentage configured
func validateThresholds(thresholds api.ResourceThresholds, thresholdName string) error {
	if len(thresholds) == 0 {
		return fmt.Errorf("%s must not be empty", thresholdName)
	}
	for name, percent := range thresholds {
		if percent < MinResourcePercentage || percent > MaxResourcePercentage {
			return fmt.Errorf("%v threshold in %s must be within [%v, %v] range", name, thresholdName, MinResourcePercentage, MaxResourcePercentage)
		}
	}
	return nil
}

func validateDefragmentationThresholds(lowThresholds, targetThresholds, protectionThresholds api.ResourceThresholds) error {
	// Validate if the lengths of lowThresholds, fragmentThresholds, and protectionThresholds are equal
	if len(lowThresholds) != len(targetThresholds) || len(targetThresholds) != len(protectionThresholds) {
		return fmt.Errorf("lowThresholds, fragmentThresholds, and protectionThresholds must have the same length")
	}

	// Iterate over lowThresholds to validate the thresholds
	for resourceName, lowValue := range lowThresholds {
		// Check if the resource exists in fragmentThresholds and protectionThresholds
		targetValue, targetExists := targetThresholds[resourceName]
		protectionValue, protectionExists := protectionThresholds[resourceName]

		if !targetExists || !protectionExists {
			return fmt.Errorf("resource %v not found in both fragmentThresholds and protectionThresholds", resourceName)
		}

		// Ensure the thresholds follow the rule: lowThresholds[resource] < fragmentThresholds[resource] < protectionThresholds[resource]
		if lowValue >= targetValue {
			return fmt.Errorf("lowThresholds[%v] (%v) must be less than fragmentThresholds[%v] (%v)", resourceName, lowValue, resourceName, targetValue)
		}
		if targetValue >= protectionValue {
			return fmt.Errorf("fragmentThresholds[%v] (%v) must be less than protectionThresholds[%v] (%v)", resourceName, targetValue, resourceName, protectionValue)
		}
	}

	return nil
}

// validateEvictableNamespaces validates the evictable namespaces settings.
func validateEvictableNamespaces(namespaces *api.Namespaces) error {
	if namespaces != nil {
		if len(namespaces.Include) > 0 {
			return fmt.Errorf("Only Exclude namespaces can be set, inclusion is not supported")
		}
	}
	return nil
}
