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
"""Stores the values used by each of the integration tests for replacing the
Auto Scaling-specific test variables.
"""

from e2e.bootstrap_resources import get_bootstrap_resources

def get_replacement_values():
    """Get replacement values from bootstrap resources."""
    try:
        resources = get_bootstrap_resources()
        return {
            "LAUNCH_TEMPLATE_ID": resources.LaunchTemplateID,
            "LAUNCH_TEMPLATE_VERSION": "$Latest",
            "AVAILABILITY_ZONE_1": resources.AvailabilityZone1,
            "VPC_ZONE_IDENTIFIER": resources.VPCZoneIdentifier,
        }
    except:
        # Fallback values if bootstrap hasn't run
        return {
            "LAUNCH_TEMPLATE_ID": "",
            "LAUNCH_TEMPLATE_VERSION": "$Latest",
            "AVAILABILITY_ZONE_1": "us-west-2a",
            "VPC_ZONE_IDENTIFIER": "",
        }

REPLACEMENT_VALUES = get_replacement_values()
