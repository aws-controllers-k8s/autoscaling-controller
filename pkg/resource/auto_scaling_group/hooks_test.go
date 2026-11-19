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
		enable    []string
		disable   []string
		want      []string
	}{
		{
			name: "existing metrics / omitted", current: existing, want: existing,
		},
		{
			name: "existing metrics / empty list", current: existing, requested: []string{},
			disable: existing,
		},
		{
			name: "existing metrics / same set reordered with a duplicate", current: existing,
			requested: []string{"GroupMinSize", "GroupMaxSize", "GroupMinSize"}, want: existing,
		},
		{
			name: "existing metrics / different set", current: existing,
			requested: []string{"GroupMinSize", "GroupDesiredCapacity", "GroupDesiredCapacity"},
			enable:    []string{"GroupDesiredCapacity"},
			disable:   []string{"GroupMaxSize"},
			want:      []string{"GroupDesiredCapacity", "GroupMinSize"},
		},
		{
			name: "no existing metrics / omitted",
		},
		{
			name: "no existing metrics / empty list", requested: []string{},
		},
		{
			// The observed set is empty, so explicitly requesting the same set is also [].
			name: "no existing metrics / same set as observed", requested: []string{},
		},
		{
			name:      "no existing metrics / different set",
			requested: []string{"GroupMinSize", "GroupDesiredCapacity"},
			enable:    []string{"GroupDesiredCapacity", "GroupMinSize"},
			want:      []string{"GroupDesiredCapacity", "GroupMinSize"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := metricsTestResource(tc.requested, nil)
			latest := metricsTestResource(nil, tc.current)

			// ACK uses unstructured conversion for field drift and patch handling.
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

			var calls []metricsTestCall
			if len(tc.enable) > 0 {
				calls = append(calls, metricsTestCall{action: "EnableMetricsCollection", metrics: tc.enable})
			}
			if len(tc.disable) > 0 {
				calls = append(calls, metricsTestCall{action: "DisableMetricsCollection", metrics: tc.disable})
			}
			wantDelta := len(calls) > 0
			if wantDelta {
				calls = append(calls, metricsTestDescribe(tc.want...))
			}
			rm := newMetricsTestManager(t, calls...)
			delta := newResourceDelta(desired, latest)
			if got := delta.DifferentAt(enabledMetricsDeltaPath); got != wantDelta {
				t.Fatalf("metrics delta = %t, want %t", got, wantDelta)
			}
			updated, err := rm.sdkUpdate(t.Context(), desired, latest, delta)
			if err != nil {
				t.Fatal(err)
			}
			assertRequestedMetrics(t, updated.ko.Spec.EnabledMetricNames, tc.requested)
			assertObservedMetrics(t, updated, tc.want...)
		})
	}
}

func TestEnabledMetricsUpdateWithCapacityChange(t *testing.T) {
	desired := metricsTestResource([]string{"GroupMinSize"}, nil)
	latest := metricsTestResource(nil, nil)
	desired.ko.Spec.MaxSize = aws.Int64(5)

	rm := newMetricsTestManager(t,
		metricsTestCall{action: "EnableMetricsCollection", metrics: []string{"GroupMinSize"}},
		metricsTestDescribe("GroupMinSize"),
		metricsTestCall{action: "UpdateAutoScalingGroup", maxSize: "5"},
	)
	updated, err := rm.sdkUpdate(t.Context(), desired, latest, newResourceDelta(desired, latest))
	if err != nil {
		t.Fatal(err)
	}
	assertObservedMetrics(t, updated, "GroupMinSize")
}

func TestEnabledMetricsUpdateWithInvalidMetric(t *testing.T) {
	desired := metricsTestResource([]string{"InvalidMetric"}, nil)
	latest := metricsTestResource(nil, []string{"GroupMinSize"})

	// The rejected enable must leave the existing metrics alone.
	rm := newMetricsTestManager(t, metricsTestRejectedEnable())
	updated, err := rm.Update(t.Context(), desired, latest, newResourceDelta(desired, latest))
	if !errors.Is(err, ackerr.Terminal) {
		t.Fatalf("update error = %v, want terminal error", err)
	}
	condition := ackcondition.Terminal(updated)
	if condition == nil || condition.Status != corev1.ConditionTrue ||
		!strings.Contains(aws.ToString(condition.Message), "InvalidMetric") {
		t.Fatalf("terminal condition = %v, want the AWS validation message", condition)
	}
	assertObservedMetrics(t, rm.concreteResource(updated), "GroupMinSize")
}

func TestEnabledMetricsUpdateWithStaleReadback(t *testing.T) {
	desired := metricsTestResource([]string{"GroupMinSize"}, nil)
	latest := metricsTestResource(nil, nil)

	rm := newMetricsTestManager(t,
		metricsTestCall{action: "EnableMetricsCollection", metrics: []string{"GroupMinSize"}},
		metricsTestDescribe(), // AWS has accepted the enable, but it is not visible yet.
	)
	updated, err := rm.sdkUpdate(t.Context(), desired, latest, newResourceDelta(desired, latest))
	var requeue *ackrequeue.RequeueNeededAfter
	if !errors.As(err, &requeue) {
		t.Fatalf("update error = %v, want a delayed requeue", err)
	}
	assertRequestedMetrics(t, updated.ko.Spec.EnabledMetricNames, []string{"GroupMinSize"})
	assertObservedMetrics(t, updated)
}

func TestEnabledMetricsCreate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested []string
		after     []metricsTestCall
	}{
		{name: "omitted"},
		{name: "empty list", requested: []string{}},
		{
			name: "enable after creation", requested: []string{"GroupMinSize"},
			after: []metricsTestCall{{action: "EnableMetricsCollection", metrics: []string{"GroupMinSize"}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rm := newMetricsTestManager(t, append([]metricsTestCall{{action: "CreateAutoScalingGroup"}}, tc.after...)...)
			created, err := rm.sdkCreate(t.Context(), metricsTestResource(tc.requested, nil))
			if err != nil {
				t.Fatal(err)
			}
			assertRequestedMetrics(t, created.ko.Spec.EnabledMetricNames, tc.requested)
		})
	}
}

func TestEnabledMetricsCreateWithInvalidMetric(t *testing.T) {
	rm := newMetricsTestManager(t, metricsTestCall{action: "CreateAutoScalingGroup"}, metricsTestRejectedEnable())
	created, err := rm.sdkCreate(t.Context(), metricsTestResource([]string{"InvalidMetric"}, nil))
	if !ackerr.IsPostCreateError(err) {
		t.Fatalf("create error = %v, want a post-create error", err)
	}
	if created == nil || aws.ToString(created.ko.Spec.Name) != metricsTestGroupName {
		t.Fatalf("create did not return the group identifier: %v", created)
	}
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

type metricsTestCall struct {
	action     string
	metrics    []string
	maxSize    string
	statusCode int
	body       string
}

func metricsTestDescribe(names ...string) metricsTestCall {
	var body strings.Builder
	body.WriteString("<DescribeAutoScalingGroupsResponse><DescribeAutoScalingGroupsResult><AutoScalingGroups><member>")
	fmt.Fprintf(&body, "<AutoScalingGroupName>%s</AutoScalingGroupName><EnabledMetrics>", metricsTestGroupName)
	for _, name := range names {
		fmt.Fprintf(&body, "<member><Metric>%s</Metric><Granularity>1Minute</Granularity></member>", name)
	}
	body.WriteString("</EnabledMetrics></member></AutoScalingGroups></DescribeAutoScalingGroupsResult></DescribeAutoScalingGroupsResponse>")
	return metricsTestCall{action: "DescribeAutoScalingGroups", body: body.String()}
}

func metricsTestRejectedEnable() metricsTestCall {
	return metricsTestCall{
		action:     "EnableMetricsCollection",
		metrics:    []string{"InvalidMetric"},
		statusCode: http.StatusBadRequest,
		body:       "<ErrorResponse><Error><Code>ValidationError</Code><Message>InvalidMetric is not supported</Message></Error></ErrorResponse>",
	}
}

func newMetricsTestManager(t *testing.T, calls ...metricsTestCall) *resourceManager {
	t.Helper()
	next := 0
	t.Cleanup(func() {
		if next != len(calls) {
			t.Errorf("made %d AWS calls, want %d", next, len(calls))
		}
	})
	return &resourceManager{
		metrics: ackmetrics.NewMetrics("autoscaling"),
		sdkapi: svcsdk.New(svcsdk.Options{
			Region:      "us-west-2",
			Credentials: aws.AnonymousCredentials{},
			HTTPClient: metricsHTTPClient(func(req *http.Request) (*http.Response, error) {
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				action := req.Form.Get("Action")
				if next >= len(calls) {
					t.Fatalf("unexpected AWS call %q", action)
				}
				call := calls[next]
				next++
				if action != call.action {
					t.Fatalf("AWS call %d = %q, want %q", next, action, call.action)
				}
				groupParameter := "AutoScalingGroupName"
				if action == "DescribeAutoScalingGroups" {
					groupParameter = "AutoScalingGroupNames.member.1"
				}
				if group := req.Form.Get(groupParameter); group != metricsTestGroupName {
					t.Errorf("%s group name = %q, want %q", action, group, metricsTestGroupName)
				}
				assertMetricsRequest(t, req, call)
				code, body := call.statusCode, call.body
				if code == 0 {
					code = http.StatusOK
				}
				if body == "" {
					body = "<" + action + "Response/>"
				}
				return &http.Response{
					StatusCode: code,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			}),
		}),
	}
}

func assertMetricsRequest(t *testing.T, req *http.Request, call metricsTestCall) {
	t.Helper()
	for i, name := range call.metrics {
		if got := req.Form.Get(fmt.Sprintf("Metrics.member.%d", i+1)); got != name {
			t.Errorf("%s metric %d = %q, want %q", call.action, i+1, got, name)
		}
	}
	var count int
	for key := range req.Form {
		if strings.HasPrefix(key, "Metrics.member.") {
			count++
		}
	}
	if count != len(call.metrics) {
		t.Errorf("%s sent %d metric names, want %d", call.action, count, len(call.metrics))
	}
	if call.action == "EnableMetricsCollection" && req.Form.Get("Granularity") != "1Minute" {
		t.Errorf("enable granularity = %q, want 1Minute", req.Form.Get("Granularity"))
	}
	if call.maxSize != "" && req.Form.Get("MaxSize") != call.maxSize {
		t.Errorf("update max size = %q, want %q", req.Form.Get("MaxSize"), call.maxSize)
	}
}
