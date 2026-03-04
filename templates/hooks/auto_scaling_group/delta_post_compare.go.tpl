	// Custom tag comparison that accounts for PropagateAtLaunch.
	// The generated delta skips Tags (compare.is_ignored: true) because
	// key/value comparison alone is insufficient for ASG tags. Here we
	// merge each side's Tags with their TagPropagateAtLaunch map and
	// compare the full TagDescription (key + value + propagateAtLaunch).
    desiredTagDescriptions := mergeTagDescriptions(
        a.ko.Spec.Tags,
        a.ko.Spec.TagPropagateAtLaunch,
        *a.ko.Spec.Name,
    )
    latestTagDescriptions := mergeTagDescriptions(
        b.ko.Spec.Tags,
        b.ko.Spec.TagPropagateAtLaunch,
        *b.ko.Spec.Name,
    )
    created, updated, deleted := compareTagDescriptions(desiredTagDescriptions, latestTagDescriptions)
    if len(created) > 0 || len(updated) > 0 || len(deleted) > 0 {
        delta.Add("Spec.Tags", a.ko.Spec.Tags, b.ko.Spec.Tags)
    }
