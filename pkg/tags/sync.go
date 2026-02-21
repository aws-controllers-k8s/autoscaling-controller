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

package tags

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	svcsdktypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"

	svcapitypes "github.com/aws-controllers-k8s/autoscaling-controller/apis/v1alpha1"
)

// TagsServiceAPI represents the API for AWS AutoScaling Tags operations
type TagsServiceAPI interface {
	CreateOrUpdateTags(context.Context, *svcsdk.CreateOrUpdateTagsInput, ...func(*svcsdk.Options)) (*svcsdk.CreateOrUpdateTagsOutput, error)
	DeleteTags(context.Context, *svcsdk.DeleteTagsInput, ...func(*svcsdk.Options)) (*svcsdk.DeleteTagsOutput, error)
	DescribeTags(context.Context, *svcsdk.DescribeTagsInput, ...func(*svcsdk.Options)) (*svcsdk.DescribeTagsOutput, error)
}

// Syncer handles syncing tags between the ACK resource and the AWS resource
type Syncer struct {
	client TagsServiceAPI
}

// NewSyncer returns a new Syncer object
func NewSyncer(client TagsServiceAPI) *Syncer {
	return &Syncer{client: client}
}

// GetTags returns the tags for a given resource
func (s *Syncer) GetTags(
	ctx context.Context,
	resourceID string,
) ([]*svcapitypes.Tag, error) {
	input := &svcsdk.DescribeTagsInput{
		Filters: []svcsdktypes.Filter{
			{
				Name:   aws.String("auto-scaling-group"),
				Values: []string{resourceID},
			},
		},
	}

	resp, err := s.client.DescribeTags(ctx, input)
	if err != nil {
		return nil, err
	}

	tags := []*svcapitypes.Tag{}
	for _, tag := range resp.Tags {
		tags = append(tags, &svcapitypes.Tag{
			Key:               tag.Key,
			Value:             tag.Value,
			PropagateAtLaunch: tag.PropagateAtLaunch,
			ResourceID:        tag.ResourceId,
			ResourceType:      tag.ResourceType,
		})
	}

	return tags, nil
}

// SyncTags synchronizes tags between the ACK resource and the AWS resource
func (s *Syncer) SyncTags(
	ctx context.Context,
	desired []*svcapitypes.Tag,
	latest []*svcapitypes.Tag,
	resourceID string,
	resourceType string,
) error {
	// If there are no desired tags, delete all existing tags
	if len(desired) == 0 && len(latest) > 0 {
		return s.deleteTags(ctx, latest, resourceID, resourceType)
	}

	// If there are no latest tags, create all desired tags
	if len(latest) == 0 && len(desired) > 0 {
		return s.createTags(ctx, desired, resourceID, resourceType)
	}

	// Otherwise, we need to determine which tags to add, update, or delete
	toAdd := []*svcapitypes.Tag{}
	toUpdate := []*svcapitypes.Tag{}
	toDelete := []*svcapitypes.Tag{}

	// Build a map of latest tags by key for easy lookup
	latestTagMap := map[string]*svcapitypes.Tag{}
	for _, tag := range latest {
		if tag.Key != nil {
			latestTagMap[*tag.Key] = tag
		}
	}

	// Build a map of desired tags by key for easy lookup
	desiredTagMap := map[string]*svcapitypes.Tag{}
	for _, tag := range desired {
		if tag.Key != nil {
			desiredTagMap[*tag.Key] = tag
		}
	}

	// Find tags to add or update
	for key, desiredTag := range desiredTagMap {
		latestTag, exists := latestTagMap[key]
		if !exists {
			// Tag doesn't exist, add it
			toAdd = append(toAdd, desiredTag)
		} else if !tagEquals(desiredTag, latestTag) {
			// Tag exists but has different value, update it
			toUpdate = append(toUpdate, desiredTag)
		}
	}

	// Find tags to delete
	for key, latestTag := range latestTagMap {
		_, exists := desiredTagMap[key]
		if !exists {
			// Tag exists in latest but not in desired, delete it
			toDelete = append(toDelete, latestTag)
		}
	}

	// Process deletions
	if len(toDelete) > 0 {
		if err := s.deleteTags(ctx, toDelete, resourceID, resourceType); err != nil {
			return err
		}
	}

	// Process additions and updates
	tagsToCreate := append(toAdd, toUpdate...)
	if len(tagsToCreate) > 0 {
		if err := s.createTags(ctx, tagsToCreate, resourceID, resourceType); err != nil {
			return err
		}
	}

	return nil
}

// createTags creates or updates tags for a resource
func (s *Syncer) createTags(
	ctx context.Context,
	tags []*svcapitypes.Tag,
	resourceID string,
	resourceType string,
) error {
	if len(tags) == 0 {
		return nil
	}

	sdkTags := []svcsdktypes.Tag{}
	for _, tag := range tags {
		// Ensure we have the resource ID and type set
		tagCopy := *tag
		if tagCopy.ResourceID == nil {
			tagCopy.ResourceID = aws.String(resourceID)
		}
		if tagCopy.ResourceType == nil {
			tagCopy.ResourceType = aws.String(resourceType)
		}
		// AWS requires PropagateAtLaunch to be set to a boolean value
		// Default to false if not specified
		if tagCopy.PropagateAtLaunch == nil {
			tagCopy.PropagateAtLaunch = aws.Bool(false)
		}

		sdkTag := svcsdktypes.Tag{
			Key:               tagCopy.Key,
			Value:             tagCopy.Value,
			PropagateAtLaunch: tagCopy.PropagateAtLaunch,
			ResourceId:        tagCopy.ResourceID,
			ResourceType:      tagCopy.ResourceType,
		}
		sdkTags = append(sdkTags, sdkTag)
	}

	_, err := s.client.CreateOrUpdateTags(
		ctx,
		&svcsdk.CreateOrUpdateTagsInput{
			Tags: sdkTags,
		},
	)
	if err != nil {
		return err
	}

	return nil
}

// deleteTags deletes tags from a resource
func (s *Syncer) deleteTags(
	ctx context.Context,
	tags []*svcapitypes.Tag,
	resourceID string,
	resourceType string,
) error {
	if len(tags) == 0 {
		return nil
	}

	sdkTags := []svcsdktypes.Tag{}
	for _, tag := range tags {
		// Ensure we have the resource ID and type set
		tagCopy := *tag
		if tagCopy.ResourceID == nil {
			tagCopy.ResourceID = aws.String(resourceID)
		}
		if tagCopy.ResourceType == nil {
			tagCopy.ResourceType = aws.String(resourceType)
		}

		sdkTag := svcsdktypes.Tag{
			Key:          tagCopy.Key,
			ResourceId:   tagCopy.ResourceID,
			ResourceType: tagCopy.ResourceType,
		}
		sdkTags = append(sdkTags, sdkTag)
	}

	_, err := s.client.DeleteTags(
		ctx,
		&svcsdk.DeleteTagsInput{
			Tags: sdkTags,
		},
	)
	if err != nil {
		return err
	}

	return nil
}

// tagEquals returns true if the two tags are equal
func tagEquals(a, b *svcapitypes.Tag) bool {
	if a == nil || b == nil {
		return a == b
	}

	// Compare the value
	if a.Value == nil && b.Value != nil {
		return false
	}
	if a.Value != nil && b.Value == nil {
		return false
	}
	if a.Value != nil && b.Value != nil && *a.Value != *b.Value {
		return false
	}

	// Compare PropagateAtLaunch
	if a.PropagateAtLaunch != nil && b.PropagateAtLaunch != nil {
		if *a.PropagateAtLaunch != *b.PropagateAtLaunch {
			return false
		}
	} else if (a.PropagateAtLaunch == nil && b.PropagateAtLaunch != nil) ||
		(a.PropagateAtLaunch != nil && b.PropagateAtLaunch == nil) {
		return false
	}

	return true
}
