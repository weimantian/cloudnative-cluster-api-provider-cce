/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhookWithManager registers the CCEClusterControllerIdentity webhook.
func (c *CCEClusterControllerIdentity) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCEClusterControllerIdentity{}).
		WithValidator(&CCEClusterControllerIdentity{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta2-cceclustercontrolleridentity,mutating=false,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=cceclustercontrolleridentities,verbs=create;update,versions=v1beta2,name=validation.cceclustercontrolleridentity.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Validator[*CCEClusterControllerIdentity] = &CCEClusterControllerIdentity{}

// ValidateCreate implements admission.Validator.
func (c *CCEClusterControllerIdentity) ValidateCreate(_ context.Context, obj *CCEClusterControllerIdentity) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (c *CCEClusterControllerIdentity) ValidateUpdate(_ context.Context, _, newObj *CCEClusterControllerIdentity) (admission.Warnings, error) {
	return nil, newObj.validate()
}

// ValidateDelete implements admission.Validator.
func (c *CCEClusterControllerIdentity) ValidateDelete(_ context.Context, _ *CCEClusterControllerIdentity) (admission.Warnings, error) {
	return nil, nil
}

func (c *CCEClusterControllerIdentity) validate() error {
	var allErrs field.ErrorList
	if c.Spec.AllowedNamespaces != nil {
		if _, err := metav1.LabelSelectorAsSelector(&c.Spec.AllowedNamespaces.Selector); err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "allowedNamespaces", "selector"),
				c.Spec.AllowedNamespaces.Selector, err.Error()))
		}
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(c.GroupVersionKind().GroupKind(), c.Name, allErrs)
}
