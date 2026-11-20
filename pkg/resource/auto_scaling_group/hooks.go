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

	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"k8s.io/apimachinery/pkg/util/sets"

	svcapitypes "github.com/aws-controllers-k8s/autoscaling-controller/apis/v1alpha1"
	"github.com/aws-controllers-k8s/autoscaling-controller/pkg/tags"
)

var syncTags = tags.Tags

const enabledMetricsDeltaPath = "Spec.EnabledMetrics"

func requestedMetricNames(metrics []*string) sets.Set[string] {
	names := sets.New[string]()
	for _, metric := range metrics {
		if metric != nil {
			names.Insert(*metric)
		}
	}
	return names
}

func enabledMetricNames(metrics []*svcapitypes.EnabledMetric) sets.Set[string] {
	names := sets.New[string]()
	for _, metric := range metrics {
		if metric != nil && metric.Metric != nil {
			names.Insert(*metric.Metric)
		}
	}
	return names
}

func (rm *resourceManager) syncMetricsCollection(
	ctx context.Context,
	groupName *string,
	requested []*string,
	enabled []*svcapitypes.EnabledMetric,
	opType string,
) error {
	desired := requestedMetricNames(requested)
	current := enabledMetricNames(enabled)

	// Neither API may receive an empty list because AWS would apply it to all
	// metrics. Enable additions first so an invalid metric leaves existing
	// collection in place.
	if additions := desired.Difference(current); len(additions) > 0 {
		_, err := rm.sdkapi.EnableMetricsCollection(ctx, &svcsdk.EnableMetricsCollectionInput{
			AutoScalingGroupName: groupName,
			Granularity:          aws.String("1Minute"),
			Metrics:              sets.List(additions),
		})
		rm.metrics.RecordAPICall(opType, "EnableMetricsCollection", err)
		if err != nil {
			return err
		}
	}

	if removals := current.Difference(desired); len(removals) > 0 {
		_, err := rm.sdkapi.DisableMetricsCollection(ctx, &svcsdk.DisableMetricsCollectionInput{
			AutoScalingGroupName: groupName,
			Metrics:              sets.List(removals),
		})
		rm.metrics.RecordAPICall(opType, "DisableMetricsCollection", err)
		if err != nil {
			return err
		}
	}
	return nil
}
