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

	ackcompare "github.com/aws-controllers-k8s/runtime/pkg/compare"
	"github.com/aws-controllers-k8s/runtime/pkg/metrics"
	ackrtlog "github.com/aws-controllers-k8s/runtime/pkg/runtime/log"
	acktags "github.com/aws-controllers-k8s/runtime/pkg/tags"
	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	svcsdktypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"

	svcapitypes "github.com/aws-controllers-k8s/autoscaling-controller/apis/v1alpha1"
)

// The type of resource. The only supported value is auto-scaling-group
const ResourceType = "auto-scaling-group"

// The default value for the PropagateAtLaunch parameter. Always set to false at initial creation/update. We later recocnile this value to match user spec
const PropogateAtLaunchDefault = false

// Tags examines the Tags in the supplied Resource and calls the
// TagResource and UntagResource APIs to ensure that the set of
// associated Tags stays in sync with the Resource.Spec.Tags
func Tags(
	ctx context.Context,
	desiredTags []*svcapitypes.Tag,
	latestTags []*svcapitypes.Tag,
	resourceID *string,
	toACKTags func([]*svcapitypes.Tag) (acktags.Tags, []string),
	sdkapi *svcsdk.Client,
	metrics *metrics.Metrics,
) (err error) {
	rlog := ackrtlog.FromContext(ctx)
	exit := rlog.Trace("syncTags")
	defer func() { exit(err) }()

	from, _ := toACKTags(latestTags)
	to, _ := toACKTags(desiredTags)

	added, _, removed := ackcompare.GetTagsDifference(from, to)

	for key := range removed {
		if _, ok := added[key]; ok {
			delete(removed, key)
		}
	}

	if len(added) > 0 {
		toAdd := make([]svcsdktypes.Tag, 0, len(added))
		for key, val := range added {
			key, val := key, val
			toAdd = append(toAdd, svcsdktypes.Tag{
				Key:               &key,
				Value:             &val,
				ResourceId:        resourceID,
				ResourceType:      aws.String(ResourceType),
				PropagateAtLaunch: aws.Bool(PropogateAtLaunchDefault),
			})
		}
		rlog.Debug("adding tags to work group", "tags", added)
		_, err = sdkapi.CreateOrUpdateTags(ctx, &svcsdk.CreateOrUpdateTagsInput{
			Tags: toAdd,
		})
		metrics.RecordAPICall("UPDATE", "AddTagsToResource", err)
		if err != nil {
			return err
		}
	}

	if len(removed) > 0 {
		toRemove := make([]svcsdktypes.Tag, 0, len(removed))
		for key := range removed {
			toRemove = append(toRemove, svcsdktypes.Tag{
				Key:          &key,
				ResourceId:   resourceID,
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

func GetTags(ctx context.Context, resourceID string, sdkapi *svcsdk.Client, metrics *metrics.Metrics) ([]*svcapitypes.Tag, error) {

	input := &svcsdk.DescribeTagsInput{
		Filters: []svcsdktypes.Filter{
			{
				Name:   aws.String(ResourceType),
				Values: []string{resourceID},
			},
		},
	}

	resp, err := sdkapi.DescribeTags(ctx, input)
	if err != nil {
		return nil, err
	}
	return convertTags(resp.Tags), nil
}

func convertTags(tags []svcsdktypes.TagDescription) []*svcapitypes.Tag {
	resp := make([]*svcapitypes.Tag, 0, len(tags))
	for _, t := range tags {
		resp = append(resp, &svcapitypes.Tag{Key: t.Key, Value: t.Value})
	}

	return resp
}
