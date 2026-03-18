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

"""Cleans up the resources created by the bootstrapping process.
"""

import logging
import boto3

from acktest.bootstrapping import Resources

from e2e import bootstrap_directory
from e2e.bootstrap_resources import BootstrapResources

def service_cleanup():
    logging.getLogger().setLevel(logging.INFO)

    try:
        resources = BootstrapResources.deserialize(bootstrap_directory)
        resources.cleanup()
        
        # Additional cleanup for launch template
        if hasattr(resources, 'LaunchTemplateID') and resources.LaunchTemplateID:
            ec2_client = boto3.client("ec2")
            try:
                ec2_client.delete_launch_template(
                    LaunchTemplateId=resources.LaunchTemplateID
                )
                logging.info(f"Deleted launch template: {resources.LaunchTemplateID}")
            except Exception as e:
                logging.error(f"Failed to delete launch template: {e}")
    except Exception as e:
        logging.warning(f"Could not load or cleanup bootstrap resources: {e}")

if __name__ == "__main__":
    service_cleanup()
