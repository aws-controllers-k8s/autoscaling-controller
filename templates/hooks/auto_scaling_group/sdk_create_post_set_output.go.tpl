	// Tags are created with the AutoScalingGroup via the CreateAutoScalingGroup API
	// No additional tag sync needed - tags are already applied
	// Set Synced to true since creation is complete
	if ko.Spec.Tags != nil {
		ackcondition.SetSynced(&resource{ko}, corev1.ConditionTrue, nil, nil)
	}
