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

// SetupWebhookWithManager registers the CCEClusterRoleIdentity webhook.
func (c *CCEClusterRoleIdentity) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCEClusterRoleIdentity{}).
		WithValidator(&CCEClusterRoleIdentity{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-cceclusterroleidentity,mutating=false,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=cceclusterroleidentities,verbs=create;update,versions=v1beta1,name=validation.cceclusterroleidentity.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Validator[*CCEClusterRoleIdentity] = &CCEClusterRoleIdentity{}

// ValidateCreate implements admission.Validator.
func (c *CCEClusterRoleIdentity) ValidateCreate(_ context.Context, obj *CCEClusterRoleIdentity) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (c *CCEClusterRoleIdentity) ValidateUpdate(_ context.Context, _, newObj *CCEClusterRoleIdentity) (admission.Warnings, error) {
	return nil, newObj.validate()
}

// ValidateDelete implements admission.Validator.
func (c *CCEClusterRoleIdentity) ValidateDelete(_ context.Context, _ *CCEClusterRoleIdentity) (admission.Warnings, error) {
	return nil, nil
}

func (c *CCEClusterRoleIdentity) validate() error {
	var allErrs field.ErrorList
	if c.Spec.AgencyName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "agencyName"), "agencyName is required"))
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(c.GroupVersionKind().GroupKind(), c.Name, allErrs)
}
