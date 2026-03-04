	if err := validateTagPropagateAtLaunch(desired.ko.Spec.Tags, desired.ko.Spec.TagPropagateAtLaunch); err != nil {
		return nil, ackerr.NewTerminalError(err)
	}

	desired.SetStatus(latest)

	if delta.DifferentAt("Spec.Tags") {
		name := string(*latest.ko.Spec.Name)
		err = syncTags(
			ctx,
			desired.ko.Spec.Tags,
			desired.ko.Spec.TagPropagateAtLaunch,
			latest.ko.Spec.Tags,
			latest.ko.Spec.TagPropagateAtLaunch,
			name,
			rm.sdkapi,
			rm.metrics,
		)
		if err != nil {
			return desired, err
		}
	}
	if !delta.DifferentExcept("Spec.Tags") {
		return desired, nil
	}
