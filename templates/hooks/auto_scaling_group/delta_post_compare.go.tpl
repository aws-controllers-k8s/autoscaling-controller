	desiredCustomTags, _ := convertToOrderedCustomTags(a.ko.Spec.Tags)
	latestCustomTags, _ := convertToOrderedCustomTags(b.ko.Spec.Tags)
	if !CustomTagsEqual(desiredCustomTags, latestCustomTags) {
		delta.Add("Spec.Tags", a.ko.Spec.Tags, b.ko.Spec.Tags)
	}