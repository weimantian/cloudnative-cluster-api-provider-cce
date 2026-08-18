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

// SetupWebhookWithManager registers the CCECluster webhook.
func (c *CCECluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCECluster{}).
		WithDefaulter(&CCECluster{}).
		WithValidator(&CCECluster{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infrastructure-cluster-x-k8s-io-v1beta1-ccecluster,mutating=true,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=create;update,versions=v1beta1,name=mutation.ccecluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-ccecluster,mutating=false,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=create;update,versions=v1beta1,name=validation.ccecluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Defaulter[*CCECluster] = &CCECluster{}

// Default implements admission.Defaulter.
func (c *CCECluster) Default(_ context.Context, _ *CCECluster) error {
	return nil
}

var _ admission.Validator[*CCECluster] = &CCECluster{}

// ValidateCreate implements admission.Validator.
func (c *CCECluster) ValidateCreate(_ context.Context, obj *CCECluster) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (c *CCECluster) ValidateUpdate(_ context.Context, _, newObj *CCECluster) (admission.Warnings, error) {
	return nil, newObj.validate()
}

// ValidateDelete implements admission.Validator.
func (c *CCECluster) ValidateDelete(_ context.Context, _ *CCECluster) (admission.Warnings, error) {
	return nil, nil
}

func (c *CCECluster) validate() error {
	var allErrs field.ErrorList
	if c.Spec.Region == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "region"), "region is required"))
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(c.GroupVersionKind().GroupKind(), c.Name, allErrs)
}
