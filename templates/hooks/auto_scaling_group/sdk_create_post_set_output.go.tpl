if len(ko.Spec.EnabledMetricNames) > 0 {
    if err = rm.syncMetricsCollection(ctx, ko.Spec.Name, ko.Spec.EnabledMetricNames, nil, "CREATE"); err != nil {
        return nil, err
    }
}
if ko.Spec.Tags != nil || ko.Spec.EnabledMetricNames != nil {
    ackcondition.SetSynced(&resource{ko}, corev1.ConditionFalse, nil, nil)
}
