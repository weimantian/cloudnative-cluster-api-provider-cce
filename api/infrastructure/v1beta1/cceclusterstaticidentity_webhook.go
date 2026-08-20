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

// SetupWebhookWithManager registers the CCEClusterStaticIdentity webhook.
func (c *CCEClusterStaticIdentity) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCEClusterStaticIdentity{}).
		WithValidator(&CCEClusterStaticIdentity{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-cceclusterstaticidentity,mutating=false,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=cceclusterstaticidentities,verbs=create;update,versions=v1beta1,name=validation.cceclusterstaticidentity.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Validator[*CCEClusterStaticIdentity] = &CCEClusterStaticIdentity{}

// ValidateCreate implements admission.Validator.
func (c *CCEClusterStaticIdentity) ValidateCreate(_ context.Context, obj *CCEClusterStaticIdentity) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (c *CCEClusterStaticIdentity) ValidateUpdate(_ context.Context, _, newObj *CCEClusterStaticIdentity) (admission.Warnings, error) {
	return nil, newObj.validate()
}

// ValidateDelete implements admission.Validator.
func (c *CCEClusterStaticIdentity) ValidateDelete(_ context.Context, _ *CCEClusterStaticIdentity) (admission.Warnings, error) {
	return nil, nil
}

func (c *CCEClusterStaticIdentity) validate() error {
	var allErrs field.ErrorList
	if c.Spec.SecretRef == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "secretRef"), "secretRef is required"))
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(c.GroupVersionKind().GroupKind(), c.Name, allErrs)
}
