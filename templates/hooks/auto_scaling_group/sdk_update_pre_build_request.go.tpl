updatedDesired := rm.concreteResource(desired.DeepCopy())
updatedDesired.SetStatus(latest)
if delta.DifferentAt("Spec.Tags") {
    name := string(*latest.ko.Spec.Name)
    err = syncTags(
        ctx, 
        desired.ko.Spec.Tags, latest.ko.Spec.Tags, 
        &name, convertToOrderedACKTags, rm.sdkapi, rm.metrics,
    )
    if err != nil {
        return nil, err
    }
}
if delta.DifferentAt(enabledMetricsDeltaPath) {
    if err = rm.syncMetricsCollection(
        ctx, latest.ko.Spec.Name, desired.ko.Spec.EnabledMetricNames, latest.ko.Status.EnabledMetrics, "UPDATE",
    ); err != nil {
        return nil, err
    }
    observed, readErr := rm.sdkFind(ctx, desired)
    if readErr != nil {
        return nil, readErr
    }
    desired.ko.Status.EnabledMetrics = observed.ko.Status.EnabledMetrics
    updatedDesired.ko.Status.EnabledMetrics = observed.ko.Status.EnabledMetrics
    if !requestedMetricNames(desired.ko.Spec.EnabledMetricNames).Equal(enabledMetricNames(observed.ko.Status.EnabledMetrics)) {
        return updatedDesired, ackrequeue.NeededAfter(
            ackerr.TemporaryOutOfSync, ackrequeue.DefaultRequeueAfterDuration,
        )
    }
}
if !delta.DifferentExcept("Spec.Tags", enabledMetricsDeltaPath) {
    return updatedDesired, nil
}
