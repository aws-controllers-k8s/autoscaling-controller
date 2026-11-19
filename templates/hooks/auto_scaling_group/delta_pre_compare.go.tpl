	// Normalize VPCZoneIdentifier on both sides before comparison.
	// AWS returns subnet IDs in a different order than submitted, causing
	// a false positive diff on every reconciliation.
	if a.ko.Spec.VPCZoneIdentifier != nil && *a.ko.Spec.VPCZoneIdentifier != "" {
		subnetsA := strings.Split(*a.ko.Spec.VPCZoneIdentifier, ",")
		sort.Strings(subnetsA)
		sortedA := strings.Join(subnetsA, ",")
		a.ko.Spec.VPCZoneIdentifier = &sortedA
	}
	if b.ko.Spec.VPCZoneIdentifier != nil && *b.ko.Spec.VPCZoneIdentifier != "" {
		subnetsB := strings.Split(*b.ko.Spec.VPCZoneIdentifier, ",")
		sort.Strings(subnetsB)
		sortedB := strings.Join(subnetsB, ",")
		b.ko.Spec.VPCZoneIdentifier = &sortedB
	}

	// Leave existing collection alone unless the user specifies a metric list.
	if a.ko.Spec.EnabledMetricNames != nil {
		requested := requestedMetricNames(a.ko.Spec.EnabledMetricNames)
		enabled := enabledMetricNames(b.ko.Status.EnabledMetrics)
		if !requested.Equal(enabled) {
			delta.Add(enabledMetricsDeltaPath, sets.List(requested), sets.List(enabled))
		}
	}
