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
"""Bootstraps the resources required to run the Auto Scaling integration tests.
"""
import logging
import boto3

from acktest.bootstrapping import Resources, BootstrapFailureException

from e2e import bootstrap_directory
from e2e.bootstrap_resources import BootstrapResources

def service_bootstrap() -> Resources:
    logging.getLogger().setLevel(logging.INFO)

    # Get EC2 client
    ec2_client = boto3.client("ec2")
    
    # Get default VPC and subnets
    vpc_id = get_default_vpc_id(ec2_client)
    subnets = get_default_subnets(ec2_client, vpc_id)
    
    if not subnets:
        raise BootstrapFailureException("No subnets found in default VPC")
    
    # Create a launch template for testing
    launch_template_name = "ack-test-asg-launch-template"
    
    try:
        # Try to create launch template
        response = ec2_client.create_launch_template(
            LaunchTemplateName=launch_template_name,
            LaunchTemplateData={
                "ImageId": get_latest_amazon_linux_ami(ec2_client),
                "InstanceType": "t3.micro",
                "TagSpecifications": [
                    {
                        "ResourceType": "instance",
                        "Tags": [
                            {"Key": "Name", "Value": "ack-test-asg-instance"},
                            {"Key": "ManagedBy", "Value": "ACK"},
                        ]
                    }
                ]
            },
            TagSpecifications=[
                {
                    "ResourceType": "launch-template",
                    "Tags": [
                        {"Key": "Name", "Value": launch_template_name},
                        {"Key": "ManagedBy", "Value": "ACK"},
                    ]
                }
            ]
        )
        launch_template_id = response["LaunchTemplate"]["LaunchTemplateId"]
        logging.info(f"Created launch template: {launch_template_id}")
    except ec2_client.exceptions.ClientError as e:
        if e.response["Error"]["Code"] == "InvalidLaunchTemplateName.AlreadyExistsException":
            # Launch template already exists, get its ID
            response = ec2_client.describe_launch_templates(
                LaunchTemplateNames=[launch_template_name]
            )
            launch_template_id = response["LaunchTemplates"][0]["LaunchTemplateId"]
            logging.info(f"Using existing launch template: {launch_template_id}")
        else:
            raise BootstrapFailureException(f"Failed to create launch template: {e}")
    
    # Get availability zones from subnets
    availability_zones = [subnet["AvailabilityZone"] for subnet in subnets]
    
    resources = BootstrapResources(
        LaunchTemplateID=launch_template_id,
        LaunchTemplateName=launch_template_name,
        AvailabilityZone1=availability_zones[0] if availability_zones else "us-west-2a",
        VPCZoneIdentifier=",".join([subnet["SubnetId"] for subnet in subnets]),
    )

    try:
        resources.bootstrap()
    except BootstrapFailureException as ex:
        exit(254)

    return resources

def get_default_vpc_id(ec2_client) -> str:
    """Get the default VPC ID."""
    response = ec2_client.describe_vpcs(
        Filters=[{"Name": "isDefault", "Values": ["true"]}]
    )
    
    if not response["Vpcs"]:
        raise BootstrapFailureException("No default VPC found")
    
    return response["Vpcs"][0]["VpcId"]

def get_default_subnets(ec2_client, vpc_id: str) -> list:
    """Get subnets in the specified VPC."""
    response = ec2_client.describe_subnets(
        Filters=[{"Name": "vpc-id", "Values": [vpc_id]}]
    )
    
    return response["Subnets"]

def get_latest_amazon_linux_ami(ec2_client) -> str:
    """Get the latest Amazon Linux 2023 AMI ID."""
    response = ec2_client.describe_images(
        Owners=["amazon"],
        Filters=[
            {"Name": "name", "Values": ["al2023-ami-2023.*-x86_64"]},
            {"Name": "state", "Values": ["available"]},
        ],
    )
    
    if not response["Images"]:
        # Fallback to Amazon Linux 2
        response = ec2_client.describe_images(
            Owners=["amazon"],
            Filters=[
                {"Name": "name", "Values": ["amzn2-ami-hvm-*-x86_64-gp2"]},
                {"Name": "state", "Values": ["available"]},
            ],
        )
    
    if not response["Images"]:
        raise BootstrapFailureException("No Amazon Linux AMI found")
    
    # Sort by creation date and get the latest
    images = sorted(response["Images"], key=lambda x: x["CreationDate"], reverse=True)
    return images[0]["ImageId"]

if __name__ == "__main__":
    config = service_bootstrap()
    # Write config to current directory by default
    config.serialize(bootstrap_directory)
