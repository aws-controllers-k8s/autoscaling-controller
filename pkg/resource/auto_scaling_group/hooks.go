// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package auto_scaling_group

import (
	"context"
	"slices"
	"strings"

	svcapitypes "github.com/aws-controllers-k8s/autoscaling-controller/apis/v1alpha1"
	"github.com/aws-controllers-k8s/autoscaling-controller/pkg/tags"
	ackrt "github.com/aws-controllers-k8s/runtime/pkg/runtime"
	acktags "github.com/aws-controllers-k8s/runtime/pkg/tags"
	acktypes "github.com/aws-controllers-k8s/runtime/pkg/types"
)

const (
	// ResourceType is the type of resource for AutoScalingGroup
	ResourceType = "auto-scaling-group"
)

// getTags returns the tags for a given AutoScalingGroup
func (rm *resourceManager) getTags(
	ctx context.Context,
	resourceID string,
) []*svcapitypes.Tag {
	tagsSyncer := tags.NewSyncer(rm.sdkapi)
	tags, err := tagsSyncer.GetTags(ctx, resourceID)
	if err != nil {
		// Log error but don't fail the reconciliation
		// Tags will be empty and can be synced on next reconcile
		rm.log.V(1).Info("failed to get tags", "error", err, "resourceID", resourceID)
		return nil
	}
	return tags
}

// syncTags synchronizes tags between the ACK resource and the AWS resource
func (rm *resourceManager) syncTags(
	ctx context.Context,
	latest *resource,
	desired *resource,
) error {
	if desired.ko.Spec.Tags == nil && latest.ko.Spec.Tags == nil {
		return nil
	}

	resourceID := ""
	if latest.ko.Spec.AutoScalingGroupName != nil {
		resourceID = *latest.ko.Spec.AutoScalingGroupName
	}

	tagsSyncer := tags.NewSyncer(rm.sdkapi)
	return tagsSyncer.SyncTags(
		ctx,
		desired.ko.Spec.Tags,
		latest.ko.Spec.Tags,
		resourceID,
		ResourceType,
	)
}

// define custom type for Tags map string to svcapitypes.Tag
type CustomTags map[string]*svcapitypes.Tag

func NewCustomTags() CustomTags {
	return make(map[string]*svcapitypes.Tag)
}

func Merge(a CustomTags, b CustomTags) CustomTags {
	var result CustomTags
	// Initialize result with the first set of tags 'a'.
	// If first set is nil, initialize result with empty set of tags.
	if a == nil {
		result = NewCustomTags()
	} else {
		result = a
	}
	if b != nil && len(b) > 0 {
		// Add all the tags which are not already present in result
		for tk, tv := range b {
			if _, found := result[tk]; !found {
				result[tk] = tv
			}
		}
	}
	return result
}

func convertToOrderedCustomTags(tags []*svcapitypes.Tag) (CustomTags, []string) {
	result := NewCustomTags()
	keyOrder := []string{}

	if len(tags) == 0 {
		return result, keyOrder
	}
	for _, t := range tags {
		if t.Key != nil {
			keyOrder = append(keyOrder, *t.Key)
			result[*t.Key] = t
		}
	}

	return result, keyOrder
}

func ignoreSystemCustomTags(tags CustomTags, systemTags []string) {
	for k := range tags {
		if strings.HasPrefix(k, "aws:") ||
			slices.Contains(systemTags, k) {
			delete(tags, k)
		}
	}
}

func syncAWSCustomTags(a CustomTags, b CustomTags) {
	for k := range b {
		if strings.HasPrefix(k, "aws:") {
			a[k] = b[k]
		}
	}
}

func fromCustomTags(tags CustomTags, keyOrder []string) []*svcapitypes.Tag {
	result := []*svcapitypes.Tag{}

	for _, k := range keyOrder {
		v, ok := tags[k]
		if ok {
			tag := v
			result = append(result, tag)
			delete(tags, k)
		}
	}
	for _, v := range tags {
		tag := v
		result = append(result, tag)
	}

	return result
}

func convertAckTagsToCustomTags(ackTags acktags.Tags) CustomTags {
	if ackTags == nil {
		return nil
	}

	svcTags := NewCustomTags()
	defaultPropogateAtLaunch := false
	for k, v := range ackTags {
		key := k
		val := v

		svcTags[k] = &svcapitypes.Tag{
			Key:               &key,
			Value:             &val,
			PropagateAtLaunch: &defaultPropogateAtLaunch,
		}
	}

	return svcTags
}

func (rm *resourceManager) customEnsureTags(
	ctx context.Context,
	res acktypes.AWSResource,
	md acktypes.ServiceControllerMetadata,
) error {
	r := rm.concreteResource(res)
	if r.ko == nil {
		// Should never happen... if it does, it's buggy code.
		panic("resource manager's EnsureTags method received resource with nil CR object")
	}
	customDefaultTags := convertAckTagsToCustomTags(ackrt.GetDefaultTags(&rm.cfg, r.ko, md))
	var existingTags []*svcapitypes.Tag
	existingTags = r.ko.Spec.Tags
	resourceTags, keyOrder := convertToOrderedCustomTags(existingTags)
	tags := Merge(resourceTags, customDefaultTags)
	r.ko.Spec.Tags = fromCustomTags(tags, keyOrder)
	return nil
}

func (rm *resourceManager) CustomFilterSystemTags(res acktypes.AWSResource, systemTags []string) {
	r := rm.concreteResource(res)
	if r == nil || r.ko == nil {
		return
	}
	var existingTags []*svcapitypes.Tag
	existingTags = r.ko.Spec.Tags
	resourceTags, tagKeyOrder := convertToOrderedCustomTags(existingTags)
	ignoreSystemCustomTags(resourceTags, systemTags)
	r.ko.Spec.Tags = fromCustomTags(resourceTags, tagKeyOrder)
}

func customMirrorAWSTags(a *resource, b *resource) {
	if a == nil || a.ko == nil || b == nil || b.ko == nil {
		return
	}
	var existingLatestTags []*svcapitypes.Tag
	var existingDesiredTags []*svcapitypes.Tag
	existingDesiredTags = a.ko.Spec.Tags
	existingLatestTags = b.ko.Spec.Tags
	desiredTags, desiredTagKeyOrder := convertToOrderedCustomTags(existingDesiredTags)
	latestTags, _ := convertToOrderedCustomTags(existingLatestTags)
	syncAWSCustomTags(desiredTags, latestTags)
	a.ko.Spec.Tags = fromCustomTags(desiredTags, desiredTagKeyOrder)
}

func CustomTagsEqual(a, b CustomTags) bool {
	if len(a) != len(b) {
		return false
	}
	for key, tagA := range a {
		tagB, exists := b[key]
		if !exists {
			return false
		}
		if !tags.TagEquals(tagA, tagB) {
			return false
		}
	}

	return true
}
