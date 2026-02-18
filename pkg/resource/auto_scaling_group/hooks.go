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

	svcapitypes "github.com/aws-controllers-k8s/autoscaling-controller/apis/v1alpha1"
	"github.com/aws-controllers-k8s/autoscaling-controller/pkg/tags"
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
