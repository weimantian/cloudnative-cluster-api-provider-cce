/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhookWithManager registers the CCEManagedMachinePool webhook.
func (m *CCEManagedMachinePool) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCEManagedMachinePool{}).
		WithDefaulter(&CCEManagedMachinePool{}).
		WithValidator(&CCEManagedMachinePool{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infrastructure-cluster-x-k8s-io-v1beta1-ccemanagedmachinepool,mutating=true,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=ccemanagedmachinepools,verbs=create;update,versions=v1beta1,name=mutation.ccemanagedmachinepool.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-ccemanagedmachinepool,mutating=false,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=ccemanagedmachinepools,verbs=create;update,versions=v1beta1,name=validation.ccemanagedmachinepool.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Defaulter[*CCEManagedMachinePool] = &CCEManagedMachinePool{}

// Default implements admission.Defaulter.
func (m *CCEManagedMachinePool) Default(_ context.Context, obj *CCEManagedMachinePool) error {
	if obj.Spec.NodePoolName == "" {
		obj.Spec.NodePoolName = obj.Name
	}
	return nil
}

var _ admission.Validator[*CCEManagedMachinePool] = &CCEManagedMachinePool{}

// ValidateCreate implements admission.Validator.
func (m *CCEManagedMachinePool) ValidateCreate(_ context.Context, obj *CCEManagedMachinePool) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (m *CCEManagedMachinePool) ValidateUpdate(_ context.Context, _, newObj *CCEManagedMachinePool) (admission.Warnings, error) {
	return nil, newObj.validate()
}

// ValidateDelete implements admission.Validator.
func (m *CCEManagedMachinePool) ValidateDelete(_ context.Context, _ *CCEManagedMachinePool) (admission.Warnings, error) {
	return nil, nil
}

func (m *CCEManagedMachinePool) validate() error {
	var allErrs field.ErrorList

	if m.Spec.ClusterName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "clusterName"), "clusterName is required"))
	}
	if m.Spec.Flavor == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "flavor"), "flavor is required"))
	}
	// Official constraint: max 20 taints.
	if len(m.Spec.Taints) > 20 {
		allErrs = append(allErrs, field.TooMany(field.NewPath("spec", "taints"), len(m.Spec.Taints), 20))
	}
	// Official constraint: Turbo >= 1.21 node pools bind max 5 security groups.
	if len(m.Spec.SecurityGroups) > 5 {
		allErrs = append(allErrs, field.TooMany(field.NewPath("spec", "securityGroups"), len(m.Spec.SecurityGroups), 5))
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(m.GroupVersionKind().GroupKind(), m.Name, allErrs)
}
