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
	"fmt"

	"github.com/aws-controllers-k8s/runtime/pkg/metrics"
	ackrtlog "github.com/aws-controllers-k8s/runtime/pkg/runtime/log"
	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	svcsdktypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"

	svcapitypes "github.com/aws-controllers-k8s/autoscaling-controller/apis/v1alpha1"
)

// The type of resource. The only supported value is auto-scaling-group
const ResourceType = "auto-scaling-group"

// The default value for the PropagateAtLaunch parameter. Always set to false at initial creation/update. We later recocnile this value to match user spec
const PropogateAtLaunchDefault = false

// ValidateTagPropagateAtLaunch checks that every key in the
// propagateAtLaunch map has a corresponding entry in the tags slice.
// Returns an error listing the orphaned keys if any are found.
func ValidateTagPropagateAtLaunch(
	tags []*svcapitypes.Tag,
	propagateAtLaunch map[string]*bool,
) error {
	if len(propagateAtLaunch) == 0 {
		return nil
	}
	tagKeys := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		if t.Key != nil {
			tagKeys[*t.Key] = struct{}{}
		}
	}
	var orphaned []string
	for k := range propagateAtLaunch {
		if _, ok := tagKeys[k]; !ok {
			orphaned = append(orphaned, k)
		}
	}
	if len(orphaned) > 0 {
		return fmt.Errorf(
			"tagPropagateAtLaunch contains keys not present in tags: %v",
			orphaned,
		)
	}
	return nil
}

// SyncTags merges desired and latest tags with their respective
// PropagateAtLaunch maps, computes the created/updated/deleted diff
// using CompareTagDescriptions, and applies the changes via
// CreateOrUpdateTags and DeleteTags.
func SyncTags(
	ctx context.Context,
	desiredTags []*svcapitypes.Tag,
	desiredPropagateAtLaunch map[string]*bool,
	latestTags []*svcapitypes.Tag,
	latestPropagateAtLaunch map[string]*bool,
	resourceID string,
	sdkapi *svcsdk.Client,
	metrics *metrics.Metrics,
) (err error) {
	rlog := ackrtlog.FromContext(ctx)
	exit := rlog.Trace("syncTags")
	defer func() { exit(err) }()

	desiredTagDescriptions := MergeTagDescriptions(
		desiredTags,
		desiredPropagateAtLaunch,
		resourceID,
	)
	latestTagDescriptions := MergeTagDescriptions(
		latestTags,
		latestPropagateAtLaunch,
		resourceID,
	)

	created, updated, deleted := CompareTagDescriptions(desiredTagDescriptions, latestTagDescriptions)

	// 4. CreateOrUpdateTags for created + updated
	toUpsert := append(created, updated...)
	if len(toUpsert) > 0 {
		toAdd := make([]svcsdktypes.Tag, 0, len(toUpsert))
		for _, td := range toUpsert {
			toAdd = append(toAdd, svcsdktypes.Tag{
				Key:               td.Key,
				Value:             td.Value,
				ResourceId:        aws.String(resourceID),
				ResourceType:      aws.String(ResourceType),
				PropagateAtLaunch: td.PropagateAtLaunch,
			})
		}
		rlog.Debug("creating/updating tags on group", "count", len(toAdd))
		_, err = sdkapi.CreateOrUpdateTags(ctx, &svcsdk.CreateOrUpdateTagsInput{
			Tags: toAdd,
		})
		metrics.RecordAPICall("UPDATE", "CreateOrUpdateTags", err)
		if err != nil {
			return err
		}
	}

	// 5. DeleteTags for deleted
	if len(deleted) > 0 {
		toRemove := make([]svcsdktypes.Tag, 0, len(deleted))
		for _, td := range deleted {
			toRemove = append(toRemove, svcsdktypes.Tag{
				Key:          td.Key,
				ResourceId:   aws.String(resourceID),
				ResourceType: aws.String(ResourceType),
			})
		}
		rlog.Debug("removing tags from group", "count", len(toRemove))
		_, err = sdkapi.DeleteTags(ctx, &svcsdk.DeleteTagsInput{
			Tags: toRemove,
		})
		metrics.RecordAPICall("UPDATE", "DeleteTags", err)
		if err != nil {
			return err
		}
	}

	return nil
}

func GetTags(ctx context.Context, resourceID string, sdkapi *svcsdk.Client, metrics *metrics.Metrics) ([]svcsdktypes.TagDescription, error) {

	input := &svcsdk.DescribeTagsInput{
		Filters: []svcsdktypes.Filter{
			{
				Name:   aws.String("auto-scaling-group"),
				Values: []string{resourceID},
			},
		},
	}

	resp, err := sdkapi.DescribeTags(ctx, input)
	if err != nil {
		return nil, err
	}
	return resp.Tags, nil
}

// MergeTagDescriptions builds a []svcsdktypes.TagDescription from the
// spec tags and the TagPropagateAtLaunch map. Each desired tag is
// converted to a TagDescription with ResourceId, ResourceType, and
// PropagateAtLaunch set. If a tag key exists in propagateAtLaunch, that
// value is used; otherwise PropagateAtLaunch defaults to false.
func MergeTagDescriptions(
	tags []*svcapitypes.Tag,
	propagateAtLaunch map[string]*bool,
	resourceID string,
) []svcsdktypes.TagDescription {
	result := make([]svcsdktypes.TagDescription, 0, len(tags))
	for _, t := range tags {
		if t.Key == nil {
			continue
		}
		td := svcsdktypes.TagDescription{
			Key:               t.Key,
			ResourceId:        aws.String(resourceID),
			ResourceType:      aws.String(ResourceType),
			PropagateAtLaunch: aws.Bool(PropogateAtLaunchDefault),
		}
		if t.Value != nil {
			td.Value = t.Value
		}
		if propagateAtLaunch != nil {
			if v, ok := propagateAtLaunch[*t.Key]; ok && v != nil {
				td.PropagateAtLaunch = v
			}
		}
		result = append(result, td)
	}
	return result
}

// CompareTagDescriptions compares desired vs latest TagDescription slices and
// returns three slices:
//   - created: tags present in desired but not in latest (new tags)
//   - updated: tags present in both but with different value or PropagateAtLaunch
//   - deleted: tags present in latest but not in desired (removed tags)
//
// Comparison is order-independent and keyed on the tag Key field.
// ResourceId and ResourceType are not compared since they are always
// the same for a given ASG.
func CompareTagDescriptions(
	desired, latest []svcsdktypes.TagDescription,
) (created, updated, deleted []svcsdktypes.TagDescription) {
	type tagInfo struct {
		Value             string
		PropagateAtLaunch bool
		Original          svcsdktypes.TagDescription
	}

	latestMap := make(map[string]tagInfo, len(latest))
	for _, t := range latest {
		key := aws.ToString(t.Key)
		latestMap[key] = tagInfo{
			Value:             aws.ToString(t.Value),
			PropagateAtLaunch: aws.ToBool(t.PropagateAtLaunch),
			Original:          t,
		}
	}

	desiredKeys := make(map[string]struct{}, len(desired))
	for _, t := range desired {
		key := aws.ToString(t.Key)
		desiredKeys[key] = struct{}{}

		li, exists := latestMap[key]
		if !exists {
			created = append(created, t)
			continue
		}
		if aws.ToString(t.Value) != li.Value || aws.ToBool(t.PropagateAtLaunch) != li.PropagateAtLaunch {
			updated = append(updated, t)
		}
	}

	for _, t := range latest {
		key := aws.ToString(t.Key)
		if _, exists := desiredKeys[key]; !exists {
			deleted = append(deleted, t)
		}
	}

	return created, updated, deleted
}
