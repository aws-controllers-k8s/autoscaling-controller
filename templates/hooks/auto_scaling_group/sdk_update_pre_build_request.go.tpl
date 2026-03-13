updatedDesired := desired.DeepCopy()
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
if !delta.DifferentExcept("Spec.Tags") {
    return rm.concreteResource(updatedDesired), nil
}