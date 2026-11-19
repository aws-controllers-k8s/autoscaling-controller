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
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	ackmetrics "github.com/aws-controllers-k8s/runtime/pkg/metrics"
	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"k8s.io/apimachinery/pkg/runtime"

	svcapitypes "github.com/aws-controllers-k8s/autoscaling-controller/apis/v1alpha1"
)

func TestEnabledMetricsDelta(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested []*string
		wantDelta bool
	}{
		{name: "omitted"},
		{name: "empty disables collection", requested: []*string{}, wantDelta: true},
		{name: "same set", requested: aws.StringSlice([]string{"GroupMaxSize", "GroupMinSize", "GroupMaxSize"})},
		{name: "different set", requested: aws.StringSlice([]string{"GroupDesiredCapacity"}), wantDelta: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := &svcapitypes.AutoScalingGroup{
				Spec: svcapitypes.AutoScalingGroupSpec{EnabledMetricNames: tc.requested},
			}
			// ACK uses unstructured conversion for field drift and patch handling.
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desired)
			if err != nil {
				t.Fatal(err)
			}
			var roundTrip svcapitypes.AutoScalingGroup
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object, &roundTrip); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(roundTrip.Spec.EnabledMetricNames, tc.requested) {
				t.Fatalf("metric names after round trip = %v, want %v", roundTrip.Spec.EnabledMetricNames, tc.requested)
			}

			latest := &svcapitypes.AutoScalingGroup{
				Status: svcapitypes.AutoScalingGroupStatus{EnabledMetrics: []*svcapitypes.EnabledMetric{
					{Metric: aws.String("GroupMinSize")},
					{Metric: aws.String("GroupMaxSize")},
				}},
			}
			delta := newResourceDelta(&resource{&roundTrip}, &resource{latest})
			if got := delta.DifferentAt(enabledMetricsDeltaPath); got != tc.wantDelta {
				t.Fatalf("metrics delta = %t, want %t", got, tc.wantDelta)
			}
		})
	}
}

type metricsHTTPClient func(*http.Request) (*http.Response, error)

func (f metricsHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSyncMetricsCollection(t *testing.T) {
	var calls []string
	rm := &resourceManager{
		metrics: ackmetrics.NewMetrics("autoscaling"),
		sdkapi: svcsdk.New(svcsdk.Options{
			Region:      "us-west-2",
			Credentials: aws.AnonymousCredentials{},
			HTTPClient: metricsHTTPClient(func(req *http.Request) (*http.Response, error) {
				if err := req.ParseForm(); err != nil {
					return nil, err
				}
				action := req.Form.Get("Action")
				calls = append(calls, action)
				if group := req.Form.Get("AutoScalingGroupName"); group != "test-asg" {
					t.Errorf("group name = %q, want test-asg", group)
				}
				metric := req.Form.Get("Metrics.member.1")
				switch action {
				case "EnableMetricsCollection":
					if granularity := req.Form.Get("Granularity"); granularity != "1Minute" || metric != "GroupMaxSize" {
						t.Errorf("enable request granularity = %q, metric = %q", granularity, metric)
					}
				case "DisableMetricsCollection":
					if metric != "GroupMinSize" {
						t.Errorf("disable request metric = %q, want GroupMinSize", metric)
					}
				}
				if req.Form.Has("Metrics.member.2") {
					t.Errorf("%s sent an unexpected second metric", action)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("<" + action + "Response/>")),
				}, nil
			}),
		}),
	}
	err := rm.syncMetricsCollection(t.Context(), aws.String("test-asg"),
		aws.StringSlice([]string{"GroupDesiredCapacity", "GroupMaxSize", "GroupMaxSize"}),
		[]*svcapitypes.EnabledMetric{{Metric: aws.String("GroupDesiredCapacity")}, {Metric: aws.String("GroupMinSize")}},
		"UPDATE",
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"EnableMetricsCollection", "DisableMetricsCollection"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("API calls = %v, want %v", calls, want)
	}
}
