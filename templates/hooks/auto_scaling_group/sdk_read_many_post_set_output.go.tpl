// DescribeAutoScalingGroups returns an empty VPCZoneIdentifier for groups
// created with availabilityZones and no subnets. The SDK decodes that as "",
// which late initialization would copy into the spec. A later spec change
// would then send "" to AWS, which rejects it with a terminal ValidationError.
// Treat an empty value as unset.
if ko.Spec.VPCZoneIdentifier != nil && *ko.Spec.VPCZoneIdentifier == "" {
	ko.Spec.VPCZoneIdentifier = nil
}
