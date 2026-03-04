	if err := validateTagPropagateAtLaunch(desired.ko.Spec.Tags, desired.ko.Spec.TagPropagateAtLaunch); err != nil {
		return nil, ackerr.NewTerminalError(err)
	}