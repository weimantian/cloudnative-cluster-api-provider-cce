/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhookWithManager registers the CCEManagedMachinePoolTemplate webhook.
func (t *CCEManagedMachinePoolTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCEManagedMachinePoolTemplate{}).
		WithDefaulter(&CCEManagedMachinePoolTemplate{}).
		WithValidator(&CCEManagedMachinePoolTemplate{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infrastructure-cluster-x-k8s-io-v1beta2-ccemanagedmachinepooltemplate,mutating=true,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=ccemanagedmachinepooltemplates,verbs=create;update,versions=v1beta2,name=mutation.ccemanagedmachinepooltemplate.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta2-ccemanagedmachinepooltemplate,mutating=false,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=ccemanagedmachinepooltemplates,verbs=create;update,versions=v1beta2,name=validation.ccemanagedmachinepooltemplate.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Defaulter[*CCEManagedMachinePoolTemplate] = &CCEManagedMachinePoolTemplate{}

// Default implements admission.Defaulter.
func (t *CCEManagedMachinePoolTemplate) Default(_ context.Context, obj *CCEManagedMachinePoolTemplate) error {
	// NodePoolName is NOT defaulted from the template name: the topology
	// controller generates the pool object name; NodePoolName is set via a
	// patch/naming strategy. Only the pure spec default is applied here.
	if obj.Spec.Template.Spec.UpdateConfig.MaxUnavailable == 0 {
		obj.Spec.Template.Spec.UpdateConfig.MaxUnavailable = 1
	}
	return nil
}

var _ admission.Validator[*CCEManagedMachinePoolTemplate] = &CCEManagedMachinePoolTemplate{}

// ValidateCreate implements admission.Validator.
func (t *CCEManagedMachinePoolTemplate) ValidateCreate(_ context.Context, obj *CCEManagedMachinePoolTemplate) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (t *CCEManagedMachinePoolTemplate) ValidateUpdate(_ context.Context, _, newObj *CCEManagedMachinePoolTemplate) (admission.Warnings, error) {
	return nil, newObj.validate()
}

// ValidateDelete implements admission.Validator.
func (t *CCEManagedMachinePoolTemplate) ValidateDelete(_ context.Context, _ *CCEManagedMachinePoolTemplate) (admission.Warnings, error) {
	return nil, nil
}

// validate delegates to the CCEManagedMachinePool validation on the wrapped
// spec, so a bad template is rejected when the ClusterClass is applied.
func (t *CCEManagedMachinePoolTemplate) validate() error {
	c := &CCEManagedMachinePool{
		TypeMeta:   metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "CCEManagedMachinePool"},
		ObjectMeta: metav1.ObjectMeta{Name: t.Name},
		Spec:       t.Spec.Template.Spec,
	}
	// clusterName is filled by the topology controller via a patch (builtin
	// {{ .clusterName }}), so it is legitimately empty on the template;
	// supply a placeholder to bypass only that one required-field check.
	if c.Spec.ClusterName == "" {
		c.Spec.ClusterName = t.Name
	}
	return c.validate()
}
