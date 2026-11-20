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
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	ackcondition "github.com/aws-controllers-k8s/runtime/pkg/condition"
	ackerr "github.com/aws-controllers-k8s/runtime/pkg/errors"
	ackmetrics "github.com/aws-controllers-k8s/runtime/pkg/metrics"
	ackrequeue "github.com/aws-controllers-k8s/runtime/pkg/requeue"
	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	svcapitypes "github.com/aws-controllers-k8s/autoscaling-controller/apis/v1alpha1"
)

const metricsTestGroupName = "test-asg"

func TestEnabledMetricsUpdate(t *testing.T) {
	existing := []string{"GroupMaxSize", "GroupMinSize"}
	for _, tc := range []struct {
		name      string
		current   []string
		requested []string
		want      []string
		calls     []string
	}{
		{name: "existing metrics / omitted", current: existing, want: existing},
		{
			name: "existing metrics / empty list", current: existing, requested: []string{},
			calls: []string{"DisableMetricsCollection GroupMaxSize GroupMinSize", "DescribeAutoScalingGroups"},
		},
		{
			name: "existing metrics / same set reordered with a duplicate", current: existing,
			requested: []string{"GroupMinSize", "GroupMaxSize", "GroupMinSize"}, want: existing,
		},
		{
			name: "existing metrics / different set", current: existing,
			requested: []string{"GroupMinSize", "GroupDesiredCapacity", "GroupDesiredCapacity"},
			want:      []string{"GroupDesiredCapacity", "GroupMinSize"},
			calls:     []string{"EnableMetricsCollection GroupDesiredCapacity", "DisableMetricsCollection GroupMaxSize", "DescribeAutoScalingGroups"},
		},
		{name: "no existing metrics / omitted"},
		{name: "no existing metrics / empty list", requested: []string{}},
		// The observed set is empty, so explicitly requesting the same set is also [].
		{name: "no existing metrics / same set as observed", requested: []string{}},
		{
			name:      "no existing metrics / different set",
			requested: []string{"GroupMinSize", "GroupDesiredCapacity"},
			want:      []string{"GroupDesiredCapacity", "GroupMinSize"},
			calls:     []string{"EnableMetricsCollection GroupDesiredCapacity GroupMinSize", "DescribeAutoScalingGroups"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := metricsTestResource(tc.requested, nil)
			latest := metricsTestResource(nil, tc.current)

			// ACK converts the resource to and from unstructured form when handling patches.
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desired.ko)
			if err != nil {
				t.Fatal(err)
			}
			var roundTrip svcapitypes.AutoScalingGroup
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object, &roundTrip); err != nil {
				t.Fatal(err)
			}
			assertRequestedMetrics(t, roundTrip.Spec.EnabledMetricNames, tc.requested)
			desired.ko = &roundTrip

			rm, calls := newMetricsTestManager(t, tc.want...)
			delta := newResourceDelta(desired, latest)
			if got, want := delta.DifferentAt(enabledMetricsDeltaPath), len(tc.calls) > 0; got != want {
				t.Fatalf("metrics delta = %t, want %t", got, want)
			}
			updated, err := rm.sdkUpdate(t.Context(), desired, latest, delta)
			if err != nil {
				t.Fatal(err)
			}
			assertCalls(t, *calls, tc.calls...)
			assertRequestedMetrics(t, updated.ko.Spec.EnabledMetricNames, tc.requested)
			assertObservedMetrics(t, updated, tc.want...)
		})
	}
}

func TestEnabledMetricsUpdateWithCapacityChange(t *testing.T) {
	desired := metricsTestResource([]string{"GroupMinSize"}, nil)
	latest := metricsTestResource(nil, nil)
	desired.ko.Spec.MaxSize = aws.Int64(5)

	rm, calls := newMetricsTestManager(t, "GroupMinSize")
	updated, err := rm.sdkUpdate(t.Context(), desired, latest, newResourceDelta(desired, latest))
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, *calls, "EnableMetricsCollection GroupMinSize", "DescribeAutoScalingGroups", "UpdateAutoScalingGroup MaxSize=5")
	assertObservedMetrics(t, updated, "GroupMinSize")
}

func TestEnabledMetricsUpdateWithInvalidMetric(t *testing.T) {
	desired := metricsTestResource([]string{"InvalidMetric"}, nil)
	latest := metricsTestResource(nil, []string{"GroupMinSize"})

	rm, calls := newMetricsTestManager(t)
	updated, err := rm.Update(t.Context(), desired, latest, newResourceDelta(desired, latest))
	if !errors.Is(err, ackerr.Terminal) {
		t.Fatalf("update error = %v, want terminal error", err)
	}
	condition := ackcondition.Terminal(updated)
	if condition == nil || condition.Status != corev1.ConditionTrue ||
		!strings.Contains(aws.ToString(condition.Message), "InvalidMetric") {
		t.Fatalf("terminal condition = %v, want the AWS validation message", condition)
	}
	// The rejected enable must leave the existing metrics alone.
	assertCalls(t, *calls, "EnableMetricsCollection InvalidMetric")
	assertObservedMetrics(t, rm.concreteResource(updated), "GroupMinSize")
}

func TestEnabledMetricsUpdateWithStaleReadback(t *testing.T) {
	desired := metricsTestResource([]string{"GroupMinSize"}, nil)
	latest := metricsTestResource(nil, nil)

	// AWS has accepted the enable, but the metrics aren't visible yet.
	rm, calls := newMetricsTestManager(t)
	updated, err := rm.sdkUpdate(t.Context(), desired, latest, newResourceDelta(desired, latest))
	var requeue *ackrequeue.RequeueNeededAfter
	if !errors.As(err, &requeue) {
		t.Fatalf("update error = %v, want a delayed requeue", err)
	}
	assertCalls(t, *calls, "EnableMetricsCollection GroupMinSize", "DescribeAutoScalingGroups")
	assertRequestedMetrics(t, updated.ko.Spec.EnabledMetricNames, []string{"GroupMinSize"})
	assertObservedMetrics(t, updated)
}

func TestEnabledMetricsCreate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested []string
		calls     []string
	}{
		{name: "omitted", calls: []string{"CreateAutoScalingGroup"}},
		{name: "empty list", requested: []string{}, calls: []string{"CreateAutoScalingGroup"}},
		{
			name: "enable after creation", requested: []string{"GroupMinSize"},
			calls: []string{"CreateAutoScalingGroup", "EnableMetricsCollection GroupMinSize"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rm, calls := newMetricsTestManager(t)
			created, err := rm.sdkCreate(t.Context(), metricsTestResource(tc.requested, nil))
			if err != nil {
				t.Fatal(err)
			}
			assertCalls(t, *calls, tc.calls...)
			assertRequestedMetrics(t, created.ko.Spec.EnabledMetricNames, tc.requested)
		})
	}
}

func TestEnabledMetricsCreateWithInvalidMetric(t *testing.T) {
	rm, calls := newMetricsTestManager(t)
	created, err := rm.sdkCreate(t.Context(), metricsTestResource([]string{"InvalidMetric"}, nil))
	if !ackerr.IsPostCreateError(err) {
		t.Fatalf("create error = %v, want a post-create error", err)
	}
	if created == nil || aws.ToString(created.ko.Spec.Name) != metricsTestGroupName {
		t.Fatalf("create did not return the group identifier: %v", created)
	}
	assertCalls(t, *calls, "CreateAutoScalingGroup", "EnableMetricsCollection InvalidMetric")
}

func metricsTestResource(requested, observed []string) *resource {
	ko := &svcapitypes.AutoScalingGroup{
		Spec: svcapitypes.AutoScalingGroupSpec{
			Name:    aws.String(metricsTestGroupName),
			MinSize: aws.Int64(0),
			MaxSize: aws.Int64(3),
		},
	}
	if requested != nil {
		ko.Spec.EnabledMetricNames = aws.StringSlice(requested)
	}
	for _, name := range observed {
		ko.Status.EnabledMetrics = append(ko.Status.EnabledMetrics, &svcapitypes.EnabledMetric{
			Metric:      aws.String(name),
			Granularity: aws.String("1Minute"),
		})
	}
	return &resource{ko}
}

func assertRequestedMetrics(t *testing.T, got []*string, want []string) {
	t.Helper()
	if (got == nil) != (want == nil) || !slices.Equal(aws.ToStringSlice(got), want) {
		t.Fatalf("spec.enabledMetrics = %v (nil=%t), want %v (nil=%t)", aws.ToStringSlice(got), got == nil, want, want == nil)
	}
}

func assertObservedMetrics(t *testing.T, got *resource, want ...string) {
	t.Helper()
	var names []string
	for _, metric := range got.ko.Status.EnabledMetrics {
		names = append(names, aws.ToString(metric.Metric))
		if granularity := aws.ToString(metric.Granularity); granularity != "1Minute" {
			t.Errorf("status granularity for %q = %q, want 1Minute", aws.ToString(metric.Metric), granularity)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, want) {
		t.Fatalf("status.enabledMetrics = %v, want %v", names, want)
	}
}

type metricsHTTPClient func(*http.Request) (*http.Response, error)

func (f metricsHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func assertCalls(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("AWS calls = %q, want %q", got, want)
	}
}

// Record the SDK requests so each test can assert what was sent after reconciliation.
func newMetricsTestManager(t *testing.T, readback ...string) (*resourceManager, *[]string) {
	t.Helper()
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
				group := req.Form.Get("AutoScalingGroupName")
				if action == "DescribeAutoScalingGroups" {
					group = req.Form.Get("AutoScalingGroupNames.member.1")
				}
				if group != metricsTestGroupName {
					t.Errorf("%s group = %q, want %q", action, group, metricsTestGroupName)
				}
				if action == "EnableMetricsCollection" && req.Form.Get("Granularity") != "1Minute" {
					t.Errorf("enable granularity = %q, want 1Minute", req.Form.Get("Granularity"))
				}

				call := []string{action}
				var metrics []string
				for key, values := range req.Form {
					if strings.HasPrefix(key, "Metrics.member.") {
						metrics = append(metrics, values...)
					}
				}
				slices.Sort(metrics)
				call = append(call, metrics...)
				if action == "UpdateAutoScalingGroup" {
					call = append(call, "MaxSize="+req.Form.Get("MaxSize"))
				}
				calls = append(calls, strings.Join(call, " "))

				code, body := http.StatusOK, "<"+action+"Response/>"
				switch {
				case action == "DescribeAutoScalingGroups":
					var entries strings.Builder
					for _, name := range readback {
						fmt.Fprintf(&entries, "<member><Metric>%s</Metric><Granularity>1Minute</Granularity></member>", name)
					}
					body = "<DescribeAutoScalingGroupsResponse><DescribeAutoScalingGroupsResult><AutoScalingGroups><member>" +
						"<AutoScalingGroupName>" + metricsTestGroupName + "</AutoScalingGroupName><EnabledMetrics>" + entries.String() +
						"</EnabledMetrics></member></AutoScalingGroups></DescribeAutoScalingGroupsResult></DescribeAutoScalingGroupsResponse>"
				case action == "EnableMetricsCollection" && slices.Contains(metrics, "InvalidMetric"):
					code = http.StatusBadRequest
					body = "<ErrorResponse><Error><Code>ValidationError</Code><Message>InvalidMetric is not supported</Message></Error></ErrorResponse>"
				}
				return &http.Response{
					StatusCode: code,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			}),
		}),
	}
	return rm, &calls
}
