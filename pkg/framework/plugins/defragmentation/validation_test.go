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
	"testing"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/descheduler/pkg/api"
)

func TestValidateDefragmentationThresholds(t *testing.T) {
	extendedResource := v1.ResourceName("example.com/foo")
	tests := []struct {
		name                 string
		lowThresholds        api.ResourceThresholds
		fragmentThresholds   api.ResourceThresholds
		protectionThresholds api.ResourceThresholds
		errInfo              error
	}{
		{
			name: "thresholds and fragmentThresholds have different number of resources",
			lowThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    20,
				v1.ResourceMemory: 20,
			},
			fragmentThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    80,
				v1.ResourceMemory: 80,
				v1.ResourcePods:   80,
			},
			protectionThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    90,
				v1.ResourceMemory: 90,
			},
			errInfo: fmt.Errorf("lowThresholds, fragmentThresholds, and protectionThresholds must have the same length"),
		},
		{
			name: "lowThresholds' CPU config value is greater than fragmentThresholds'",
			lowThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    90,
				v1.ResourceMemory: 20,
			},
			fragmentThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    80,
				v1.ResourceMemory: 80,
			},
			protectionThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    100,
				v1.ResourceMemory: 100,
			},
			errInfo: fmt.Errorf("lowThresholds[%v] (%v) must be less than fragmentThresholds[%v] (%v)", v1.ResourceCPU, 90, v1.ResourceCPU, 80),
		},
		{
			name: "lowThresholds value is greater than fragmentThresholds for memory",
			lowThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    20,
				v1.ResourceMemory: 90,
			},
			fragmentThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    80,
				v1.ResourceMemory: 80,
			},
			protectionThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    100,
				v1.ResourceMemory: 100,
			},
			errInfo: fmt.Errorf("lowThresholds[%v] (%v) must be less than fragmentThresholds[%v] (%v)", v1.ResourceMemory, 90, v1.ResourceMemory, 80),
		},
		{
			name: "thresholds, fragmentThresholds and protectionThresholds configured with extended resource",
			lowThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    20,
				v1.ResourceMemory: 20,
				extendedResource:  20,
			},
			fragmentThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    80,
				v1.ResourceMemory: 80,
				extendedResource:  80,
			},
			protectionThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    100,
				v1.ResourceMemory: 100,
				extendedResource:  100,
			},
			errInfo: nil,
		},
		{
			name: "thresholds and fragmentThresholds configured different extended resources",
			lowThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    20,
				v1.ResourceMemory: 20,
				extendedResource:  20,
			},
			fragmentThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    80,
				v1.ResourceMemory: 80,
				"example.com/bar": 80,
			},
			protectionThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    100,
				v1.ResourceMemory: 100,
			},
			errInfo: fmt.Errorf("lowThresholds, fragmentThresholds, and protectionThresholds must have the same length"),
		},
		{
			name: "passing valid thresholds, fragmentThresholds, and protectionThresholds",
			lowThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    20,
				v1.ResourceMemory: 20,
			},
			fragmentThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    70,
				v1.ResourceMemory: 70,
			},
			protectionThresholds: api.ResourceThresholds{
				v1.ResourceCPU:    90,
				v1.ResourceMemory: 90,
			},
			errInfo: nil,
		},
	}

	for _, testCase := range tests {
		args := &DefragmentationArgs{
			LowThresholds:        testCase.lowThresholds,
			FragmentThresholds:   testCase.fragmentThresholds,
			ProtectionThresholds: testCase.protectionThresholds,
		}
		err := validateDefragmentationThresholds(args.LowThresholds, args.FragmentThresholds, args.ProtectionThresholds)

		if err == nil || testCase.errInfo == nil {
			if err != testCase.errInfo {
				t.Errorf("expected validity of thresholds: lowThresholds %#v fragmentThresholds %#v protectionThresholds %#v to be %v but got %v instead",
					testCase.lowThresholds, testCase.fragmentThresholds, testCase.protectionThresholds, testCase.errInfo, err)
			}
		} else if err.Error() != testCase.errInfo.Error() {
			t.Errorf("expected validity of thresholds: lowThresholds %#v fragmentThresholds %#v protectionThresholds %#v to be %v but got %v instead",
				testCase.lowThresholds, testCase.fragmentThresholds, testCase.protectionThresholds, testCase.errInfo, err)
		}
	}
}
