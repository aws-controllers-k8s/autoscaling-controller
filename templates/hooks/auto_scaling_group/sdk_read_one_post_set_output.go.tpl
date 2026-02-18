if ko.Spec.AutoScalingGroupName != nil {
    ko.Spec.Tags = rm.getTags(ctx, *ko.Spec.AutoScalingGroupName)
}