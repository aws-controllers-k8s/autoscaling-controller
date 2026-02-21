	// Always force delete to avoid "ScalingActivityInProgress" errors
	// This allows deletion even when scaling activities are in progress
	input.ForceDelete = aws.Bool(true)
