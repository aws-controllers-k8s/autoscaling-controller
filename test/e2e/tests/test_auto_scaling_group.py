# Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#	 http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.

"""Integration tests for the AutoScalingGroup resource.
"""

import pytest
import time
import logging

from acktest.resources import random_suffix_name
from acktest.k8s import resource as k8s
from e2e import service_marker, CRD_GROUP, CRD_VERSION, load_autoscaling_resource
from e2e.replacement_values import REPLACEMENT_VALUES
from acktest import tags

RESOURCE_PLURAL = "autoscalinggroups"

@pytest.fixture(scope="module")
def simple_auto_scaling_group():
    resource_name = random_suffix_name("ack-test-asg", 32)
    
    replacements = REPLACEMENT_VALUES.copy()
    replacements["ASG_NAME"] = resource_name
    
    resource_data = load_autoscaling_resource(
        "auto_scaling_group",
        additional_replacements=replacements,
    )
    logging.debug(resource_data)

    # Create the k8s resource
    ref = k8s.CustomResourceReference(
        CRD_GROUP, CRD_VERSION, RESOURCE_PLURAL,
        resource_name, namespace="default",
    )
    k8s.create_custom_resource(ref, resource_data)
    cr = k8s.wait_resource_consumed_by_controller(ref)

    assert cr is not None
    assert k8s.get_resource_exists(ref)

    yield (ref, cr)

    # Teardown
    try:
        _, deleted = k8s.delete_custom_resource(ref, 3, 10)
        assert deleted
    except:
        pass


@service_marker
@pytest.mark.canary
class TestAutoScalingGroup:
    def test_create_delete(self, autoscaling_client, simple_auto_scaling_group):
        (ref, cr) = simple_auto_scaling_group

        # Wait for the resource to be synced
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        # Verify the resource exists in AWS
        asg_name = cr["spec"]["name"]
        
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        
        assert len(response["AutoScalingGroups"]) == 1
        asg = response["AutoScalingGroups"][0]
        
        # Verify basic properties
        assert asg["AutoScalingGroupName"] == asg_name
        assert asg["MinSize"] == cr["spec"]["minSize"]
        assert asg["MaxSize"] == cr["spec"]["maxSize"]
        assert asg["DesiredCapacity"] == cr["spec"].get("desiredCapacity", cr["spec"]["minSize"])

    def test_update_capacity(self, autoscaling_client, simple_auto_scaling_group):
        (ref, cr) = simple_auto_scaling_group
        
        # Wait for initial sync
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        asg_name = cr["spec"]["name"]
        
        # Update desired capacity
        updates = {
            "spec": {
                "desiredCapacity": 2
            }
        }
        
        k8s.patch_custom_resource(ref, updates)
        time.sleep(5)
        
        # Wait for the update to sync
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        # Verify the update in AWS
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        
        asg = response["AutoScalingGroups"][0]
        assert asg["DesiredCapacity"] == 2

    def test_update_min_max_size(self, autoscaling_client, simple_auto_scaling_group):
        (ref, cr) = simple_auto_scaling_group
        
        # Wait for initial sync
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        asg_name = cr["spec"]["name"]
        
        # Update min and max size
        updates = {
            "spec": {
                "minSize": 1,
                "maxSize": 5
            }
        }
        
        k8s.patch_custom_resource(ref, updates)
        time.sleep(5)
        
        # Wait for the update to sync
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        # Verify the update in AWS
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        
        asg = response["AutoScalingGroups"][0]
        assert asg["MinSize"] == 1
        assert asg["MaxSize"] == 5

    def test_create_delete_tags(self, autoscaling_client, simple_auto_scaling_group):        
        (ref, cr) = simple_auto_scaling_group
        modify_wait_after_seconds = 5
        
        # Wait for initial sync
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        asg_name = cr["spec"]["name"]
        
        # Get initial state
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        assert len(response["AutoScalingGroups"]) == 1
        
        cr = k8s.get_resource(ref)
        assert cr is not None
        assert "status" in cr
        
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        # Test 1: Add new tag
        updates = {
            "spec": {
                "tags": [
                    {
                        "key": "new-tag-key",
                        "value": "new-tag-value-1",
                    }
                ],
                "tagPropagateAtLaunch": {
                    "new-tag-key": True
                }
            }
        }
        
        k8s.patch_custom_resource(ref, updates)
        time.sleep(modify_wait_after_seconds)
        
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        latest_tags = response["AutoScalingGroups"][0]["Tags"]
        updated_tags = {"new-tag-key": "new-tag-value-1"}
        
        tags.assert_ack_system_tags(
            tags=latest_tags,
        )
        tags.assert_equal_without_ack_tags(
            expected=updated_tags,
            actual=latest_tags,
        )
        
        # Verify k8s resource tags don't contain system tags
        cr = k8s.get_resource(ref)
        tags.assert_equal(
            expected=updated_tags,
            actual=cr["spec"]["tags"],
            key_member_name='key',
            value_member_name='value'
        )
        
        # Test 2: Update tag value
        updates = {
            "spec": {
                "tags": [
                    {
                        "key": "new-tag-key",
                        "value": "new-tag-value-2",
                    }
                ],
                "tagPropagateAtLaunch": {
                    "new-tag-key": True
                }
            }
        }
        
        k8s.patch_custom_resource(ref, updates)
        time.sleep(modify_wait_after_seconds)
        
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        latest_tags = response["AutoScalingGroups"][0]["Tags"]
        updated_tags = {"new-tag-key": "new-tag-value-2"}
        
        tags.assert_ack_system_tags(
            tags=latest_tags,
        )
        tags.assert_equal_without_ack_tags(
            expected=updated_tags,
            actual=latest_tags,
        )

        # Verify k8s resource tags don't contain system tags
        cr = k8s.get_resource(ref)
        tags.assert_equal(
            expected=updated_tags,
            actual=cr["spec"]["tags"],
            key_member_name='key',
            value_member_name='value'
        )

        # Test 3: Update propagateAtLaunch value
        updates = {
            "spec": {
                "tags": [
                    {
                        "key": "new-tag-key",
                        "value": "new-tag-value-2",
                    }
                ],
                "tagPropagateAtLaunch": {
                    "new-tag-key": False
                }
            }
        }
        
        k8s.patch_custom_resource(ref, updates)
        time.sleep(modify_wait_after_seconds)
        
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        latest_tags = response["AutoScalingGroups"][0]["Tags"]
        
        # Verify propagateAtLaunch was updated
        tag_dict = {tag["Key"]: tag for tag in latest_tags}
        assert "new-tag-key" in tag_dict
        assert tag_dict["new-tag-key"]["Value"] == "new-tag-value-2"
        assert tag_dict["new-tag-key"]["PropagateAtLaunch"] == False
        
        # Test 4: Delete all tags
        updates = {
            "spec": {
                "tags": [],
                "tagPropagateAtLaunch": None
            }
        }
        
        k8s.patch_custom_resource(ref, updates)
        time.sleep(modify_wait_after_seconds)
        
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        latest_tags = response["AutoScalingGroups"][0]["Tags"]
        updated_tags = {}
        
        tags.assert_ack_system_tags(
            tags=latest_tags,
        )
        tags.assert_equal_without_ack_tags(
            expected=updated_tags,
            actual=latest_tags,
        )
        
        # Verify k8s resource tags are nil/empty
        cr = k8s.get_resource(ref)
        assert cr["spec"].get("tags") is None or cr["spec"].get("tags") == []

    def test_tags_set_to_nil(self, autoscaling_client, simple_auto_scaling_group):
        """Test that setting tags to nil (None) deletes all user tags from AWS."""
        (ref, cr) = simple_auto_scaling_group
        modify_wait_after_seconds = 5

        # Wait for initial sync
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)

        asg_name = cr["spec"]["name"]

        # First, add some tags
        updates = {
            "spec": {
                "tags": [
                    {
                        "key": "test-nil-tag-1",
                        "value": "value-1",
                    },
                    {
                        "key": "test-nil-tag-2",
                        "value": "value-2",
                    }
                ],
                "tagPropagateAtLaunch": {
                    "test-nil-tag-1": True,
                    "test-nil-tag-2": False
                }
            }
        }

        k8s.patch_custom_resource(ref, updates)
        time.sleep(modify_wait_after_seconds)

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)

        # Verify tags were added
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        latest_tags = response["AutoScalingGroups"][0]["Tags"]
        expected_tags = {
            "test-nil-tag-1": "value-1",
            "test-nil-tag-2": "value-2"
        }

        tags.assert_ack_system_tags(
            tags=latest_tags,
        )
        tags.assert_equal_without_ack_tags(
            expected=expected_tags,
            actual=latest_tags,
        )

        # Verify k8s resource tags don't contain system tags
        cr = k8s.get_resource(ref)
        tags.assert_equal(
            expected=expected_tags,
            actual=cr["spec"]["tags"],
            key_member_name='key',
            value_member_name='value'
        )

        # Now set tags to nil (None) - this should delete all user tags
        updates = {
            "spec": {
                "tags": None,
                "tagPropagateAtLaunch": None
            }
        }

        k8s.patch_custom_resource(ref, updates)
        time.sleep(modify_wait_after_seconds)

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)

        # Verify all user tags were deleted (only ACK system tags should remain)
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        latest_tags = response["AutoScalingGroups"][0]["Tags"]

        tags.assert_ack_system_tags(
            tags=latest_tags,
        )
        tags.assert_equal_without_ack_tags(
            expected={},
            actual=latest_tags,
        )

        # Verify k8s resource tags are nil/empty
        cr = k8s.get_resource(ref)
        assert cr["spec"].get("tags") is None or cr["spec"].get("tags") == []

    def test_propagate_at_launch_for_system_tags(self, autoscaling_client, simple_auto_scaling_group):
        """Test that a user can set propagateAtLaunch for ACK system tags
        (services.k8s.aws/controller-version, services.k8s.aws/namespace)
        via the tagPropagateAtLaunch map."""
        (ref, cr) = simple_auto_scaling_group
        modify_wait_after_seconds = 5

        # Wait for initial sync
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)

        asg_name = cr["spec"]["name"]

        # Verify system tags exist with default propagateAtLaunch (false)
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        latest_tags = response["AutoScalingGroups"][0]["Tags"]
        tags.assert_ack_system_tags(tags=latest_tags)

        tag_dict = {tag["Key"]: tag for tag in latest_tags}
        assert tag_dict["services.k8s.aws/controller-version"]["PropagateAtLaunch"] == False
        assert tag_dict["services.k8s.aws/namespace"]["PropagateAtLaunch"] == False

        # Set propagateAtLaunch to true for both system tags
        updates = {
            "spec": {
                "tagPropagateAtLaunch": {
                    "services.k8s.aws/controller-version": True,
                    "services.k8s.aws/namespace": True
                }
            }
        }

        k8s.patch_custom_resource(ref, updates)
        time.sleep(modify_wait_after_seconds)

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=10)

        # Verify system tags now have propagateAtLaunch=true in AWS
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        latest_tags = response["AutoScalingGroups"][0]["Tags"]
        tags.assert_ack_system_tags(tags=latest_tags)

        tag_dict = {tag["Key"]: tag for tag in latest_tags}
        assert tag_dict["services.k8s.aws/controller-version"]["PropagateAtLaunch"] == True
        assert tag_dict["services.k8s.aws/namespace"]["PropagateAtLaunch"] == True

    def test_orphaned_propagate_at_launch_terminal(self, autoscaling_client, simple_auto_scaling_group):
        """Test that specifying a key in tagPropagateAtLaunch that does not
        exist in tags causes the resource to enter a Terminal condition."""
        (ref, cr) = simple_auto_scaling_group
        modify_wait_after_seconds = 5

        # Wait for initial sync
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)

        # Patch with a tagPropagateAtLaunch key that has no matching tag
        updates = {
            "spec": {
                "tags": [
                    {
                        "key": "existing-tag",
                        "value": "some-value",
                    }
                ],
                "tagPropagateAtLaunch": {
                    "existing-tag": True,
                    "non-existent-tag": True
                }
            }
        }

        k8s.patch_custom_resource(ref, updates)
        time.sleep(modify_wait_after_seconds)

        # Verify the resource enters a Terminal condition
        terminal_condition = k8s.get_resource_condition(ref, "ACK.Terminal")
        assert terminal_condition is not None, \
            "Expected ACK.Terminal condition to be set"
        assert str(terminal_condition.get("status")) == "True", \
            f"Expected ACK.Terminal status True, got {terminal_condition.get('status')}"
        assert "non-existent-tag" in terminal_condition.get("message", ""), \
            f"Expected error message to mention orphaned key, got: {terminal_condition.get('message')}"

        # Clean up: remove the invalid tagPropagateAtLaunch to recover
        updates = {
            "spec": {
                "tags": None,
                "tagPropagateAtLaunch": None
            }
        }

        k8s.patch_custom_resource(ref, updates)
        time.sleep(modify_wait_after_seconds)

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=10)



    def test_delete(self, autoscaling_client):
        """Test that deleting the K8s resource deletes the AWS AutoScalingGroup."""
        resource_name = random_suffix_name("ack-test-asg-del", 32)
        
        replacements = REPLACEMENT_VALUES.copy()
        replacements["ASG_NAME"] = resource_name
        
        resource_data = load_autoscaling_resource(
            "auto_scaling_group",
            additional_replacements=replacements,
        )
        
        # Create the k8s resource
        ref = k8s.CustomResourceReference(
            CRD_GROUP, CRD_VERSION, RESOURCE_PLURAL,
            resource_name, namespace="default",
        )
        k8s.create_custom_resource(ref, resource_data)
        cr = k8s.wait_resource_consumed_by_controller(ref)
        
        assert cr is not None
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=3)
        
        asg_name = cr["spec"]["name"]
        
        # Verify the ASG exists in AWS
        response = autoscaling_client.describe_auto_scaling_groups(
            AutoScalingGroupNames=[asg_name]
        )
        assert len(response["AutoScalingGroups"]) == 1
        
        # Delete the K8s resource
        _, deleted = k8s.delete_custom_resource(ref, 3, 10)
        assert deleted
        
        # Poll for AWS deletion to complete (can take time with scaling activities)
        max_wait_periods = 60  # 30 * 10 seconds = 5 minutes max
        wait_period_length = 10
        
        for attempt in range(max_wait_periods):
            time.sleep(wait_period_length)
            
            response = autoscaling_client.describe_auto_scaling_groups(
                AutoScalingGroupNames=[asg_name]
            )
            
            if len(response["AutoScalingGroups"]) == 0:
                # Successfully deleted
                return
        
        # If we get here, deletion didn't complete in time
        assert False, f"AutoScalingGroup {asg_name} was not deleted from AWS after {max_wait_periods * wait_period_length} seconds"
