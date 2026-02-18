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

package tags_test

import (
	"context"
	"testing"

	"github.com/aws-controllers-k8s/autoscaling-controller/pkg/tags"
	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/autoscaling"
	svcsdktypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	svcapitypes "github.com/aws-controllers-k8s/autoscaling-controller/apis/v1alpha1"
)

type mockTagsClient struct {
	mock.Mock
}

func (m *mockTagsClient) CreateOrUpdateTags(
	ctx context.Context,
	input *svcsdk.CreateOrUpdateTagsInput,
	opts ...func(*svcsdk.Options),
) (*svcsdk.CreateOrUpdateTagsOutput, error) {
	args := m.Called(ctx, input)
	return args.Get(0).(*svcsdk.CreateOrUpdateTagsOutput), args.Error(1)
}

func (m *mockTagsClient) DeleteTags(
	ctx context.Context,
	input *svcsdk.DeleteTagsInput,
	opts ...func(*svcsdk.Options),
) (*svcsdk.DeleteTagsOutput, error) {
	args := m.Called(ctx, input)
	return args.Get(0).(*svcsdk.DeleteTagsOutput), args.Error(1)
}

func (m *mockTagsClient) DescribeTags(
	ctx context.Context,
	input *svcsdk.DescribeTagsInput,
	opts ...func(*svcsdk.Options),
) (*svcsdk.DescribeTagsOutput, error) {
	args := m.Called(ctx, input)
	return args.Get(0).(*svcsdk.DescribeTagsOutput), args.Error(1)
}

func TestGetTags(t *testing.T) {
	mockClient := &mockTagsClient{}
	syncer := tags.NewSyncer(mockClient)

	ctx := context.Background()
	resourceID := "test-asg"

	expectedInput := &svcsdk.DescribeTagsInput{
		Filters: []svcsdktypes.Filter{
			{
				Name:   aws.String("auto-scaling-group"),
				Values: []string{resourceID},
			},
		},
	}

	expectedOutput := &svcsdk.DescribeTagsOutput{
		Tags: []svcsdktypes.TagDescription{
			{
				Key:               aws.String("Name"),
				Value:             aws.String("test-asg"),
				PropagateAtLaunch: aws.Bool(true),
				ResourceId:        aws.String(resourceID),
				ResourceType:      aws.String("auto-scaling-group"),
			},
			{
				Key:               aws.String("Environment"),
				Value:             aws.String("test"),
				PropagateAtLaunch: aws.Bool(true),
				ResourceId:        aws.String(resourceID),
				ResourceType:      aws.String("auto-scaling-group"),
			},
		},
	}

	mockClient.On("DescribeTags", ctx, expectedInput).Return(expectedOutput, nil)

	result, err := syncer.GetTags(ctx, resourceID)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "Name", *result[0].Key)
	assert.Equal(t, "test-asg", *result[0].Value)
	assert.Equal(t, "Environment", *result[1].Key)
	assert.Equal(t, "test", *result[1].Value)
	mockClient.AssertExpectations(t)
}

func TestSyncTags_CreateTags(t *testing.T) {
	mockClient := &mockTagsClient{}
	syncer := tags.NewSyncer(mockClient)

	ctx := context.Background()
	resourceID := "test-asg"
	resourceType := "auto-scaling-group"

	desired := []*svcapitypes.Tag{
		{
			Key:               aws.String("Name"),
			Value:             aws.String("test-asg"),
			PropagateAtLaunch: aws.Bool(true),
		},
		{
			Key:               aws.String("Environment"),
			Value:             aws.String("test"),
			PropagateAtLaunch: aws.Bool(true),
		},
	}

	latest := []*svcapitypes.Tag{}

	expectedInput := &svcsdk.CreateOrUpdateTagsInput{
		Tags: []svcsdktypes.Tag{
			{
				Key:               aws.String("Name"),
				Value:             aws.String("test-asg"),
				PropagateAtLaunch: aws.Bool(true),
				ResourceId:        aws.String(resourceID),
				ResourceType:      aws.String(resourceType),
			},
			{
				Key:               aws.String("Environment"),
				Value:             aws.String("test"),
				PropagateAtLaunch: aws.Bool(true),
				ResourceId:        aws.String(resourceID),
				ResourceType:      aws.String(resourceType),
			},
		},
	}

	mockClient.On("CreateOrUpdateTags", ctx, mock.MatchedBy(func(input *svcsdk.CreateOrUpdateTagsInput) bool {
		if len(input.Tags) != len(expectedInput.Tags) {
			return false
		}
		for i, tag := range input.Tags {
			if *tag.Key != *expectedInput.Tags[i].Key ||
				*tag.Value != *expectedInput.Tags[i].Value ||
				*tag.ResourceId != *expectedInput.Tags[i].ResourceId ||
				*tag.ResourceType != *expectedInput.Tags[i].ResourceType {
				return false
			}
		}
		return true
	})).Return(&svcsdk.CreateOrUpdateTagsOutput{}, nil)

	err := syncer.SyncTags(ctx, desired, latest, resourceID, resourceType)

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestSyncTags_DeleteTags(t *testing.T) {
	mockClient := &mockTagsClient{}
	syncer := tags.NewSyncer(mockClient)

	ctx := context.Background()
	resourceID := "test-asg"
	resourceType := "auto-scaling-group"

	desired := []*svcapitypes.Tag{}

	latest := []*svcapitypes.Tag{
		{
			Key:               aws.String("Name"),
			Value:             aws.String("test-asg"),
			PropagateAtLaunch: aws.Bool(true),
			ResourceID:        aws.String(resourceID),
			ResourceType:      aws.String(resourceType),
		},
		{
			Key:               aws.String("Environment"),
			Value:             aws.String("test"),
			PropagateAtLaunch: aws.Bool(true),
			ResourceID:        aws.String(resourceID),
			ResourceType:      aws.String(resourceType),
		},
	}

	expectedInput := &svcsdk.DeleteTagsInput{
		Tags: []svcsdktypes.Tag{
			{
				Key:          aws.String("Name"),
				ResourceId:   aws.String(resourceID),
				ResourceType: aws.String(resourceType),
			},
			{
				Key:          aws.String("Environment"),
				ResourceId:   aws.String(resourceID),
				ResourceType: aws.String(resourceType),
			},
		},
	}

	mockClient.On("DeleteTags", ctx, mock.MatchedBy(func(input *svcsdk.DeleteTagsInput) bool {
		if len(input.Tags) != len(expectedInput.Tags) {
			return false
		}
		for i, tag := range input.Tags {
			if *tag.Key != *expectedInput.Tags[i].Key ||
				*tag.ResourceId != *expectedInput.Tags[i].ResourceId ||
				*tag.ResourceType != *expectedInput.Tags[i].ResourceType {
				return false
			}
		}
		return true
	})).Return(&svcsdk.DeleteTagsOutput{}, nil)

	err := syncer.SyncTags(ctx, desired, latest, resourceID, resourceType)

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestSyncTags_UpdateTags(t *testing.T) {
	mockClient := &mockTagsClient{}
	syncer := tags.NewSyncer(mockClient)

	ctx := context.Background()
	resourceID := "test-asg"
	resourceType := "auto-scaling-group"

	desired := []*svcapitypes.Tag{
		{
			Key:               aws.String("Name"),
			Value:             aws.String("updated-asg"),
			PropagateAtLaunch: aws.Bool(true),
		},
		{
			Key:               aws.String("NewTag"),
			Value:             aws.String("new-value"),
			PropagateAtLaunch: aws.Bool(true),
		},
	}

	latest := []*svcapitypes.Tag{
		{
			Key:               aws.String("Name"),
			Value:             aws.String("test-asg"),
			PropagateAtLaunch: aws.Bool(true),
			ResourceID:        aws.String(resourceID),
			ResourceType:      aws.String(resourceType),
		},
		{
			Key:               aws.String("Environment"),
			Value:             aws.String("test"),
			PropagateAtLaunch: aws.Bool(true),
			ResourceID:        aws.String(resourceID),
			ResourceType:      aws.String(resourceType),
		},
	}

	// Expect delete for the Environment tag
	mockClient.On("DeleteTags", ctx, mock.MatchedBy(func(input *svcsdk.DeleteTagsInput) bool {
		if len(input.Tags) != 1 {
			return false
		}
		return *input.Tags[0].Key == "Environment"
	})).Return(&svcsdk.DeleteTagsOutput{}, nil)

	// Expect create/update for the Name and NewTag tags
	mockClient.On("CreateOrUpdateTags", ctx, mock.MatchedBy(func(input *svcsdk.CreateOrUpdateTagsInput) bool {
		if len(input.Tags) != 2 {
			return false
		}

		hasNameTag := false
		hasNewTag := false

		for _, tag := range input.Tags {
			if *tag.Key == "Name" && *tag.Value == "updated-asg" {
				hasNameTag = true
			}
			if *tag.Key == "NewTag" && *tag.Value == "new-value" {
				hasNewTag = true
			}
		}

		return hasNameTag && hasNewTag
	})).Return(&svcsdk.CreateOrUpdateTagsOutput{}, nil)

	err := syncer.SyncTags(ctx, desired, latest, resourceID, resourceType)

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}
