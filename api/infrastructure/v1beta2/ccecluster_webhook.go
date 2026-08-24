/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"context"
	"net"

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

// +kubebuilder:webhook:path=/mutate-infrastructure-cluster-x-k8s-io-v1beta2-ccecluster,mutating=true,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=create;update,versions=v1beta2,name=mutation.ccecluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta2-ccecluster,mutating=false,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=create;update,versions=v1beta2,name=validation.ccecluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

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
func (c *CCECluster) ValidateUpdate(_ context.Context, oldObj, newObj *CCECluster) (admission.Warnings, error) {
	var allErrs field.ErrorList
	// Region is immutable: the region determines every downstream cloud
	// resource location and cannot change after creation.
	if oldObj.Spec.Region != newObj.Spec.Region {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "region"),
			newObj.Spec.Region, "field is immutable after creation"))
	}
	// Once a BYO VPC is referenced it cannot be swapped for another VPC
	// (CCE requires a VPC before cluster creation; swapping would orphan nodes).
	if oldObj.Spec.Network.VPC.ID != "" && oldObj.Spec.Network.VPC.ID != newObj.Spec.Network.VPC.ID {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "network", "vpc", "id"),
			newObj.Spec.Network.VPC.ID, "field is immutable after creation"))
	}
	if err := newObj.validate(); err != nil {
		return nil, err
	}
	if len(allErrs) > 0 {
		return nil, apierrors.NewInvalid(c.GroupVersionKind().GroupKind(), c.Name, allErrs)
	}
	return nil, nil
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
	// VPC CIDR format (only meaningful when the VPC is to be created).
	if c.Spec.Network.VPC.CIDR != "" {
		if _, _, err := net.ParseCIDR(c.Spec.Network.VPC.CIDR); err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "network", "vpc", "cidr"),
				c.Spec.Network.VPC.CIDR, "must be a valid IPv4/IPv6 CIDR (e.g. 10.0.0.0/16)"))
		}
	}
	// Each subnet CIDR must be valid (only meaningful when created).
	for i, sn := range c.Spec.Network.Subnets {
		if sn.CIDR == "" {
			continue
		}
		p := field.NewPath("spec", "network", "subnets").Index(i).Child("cidr")
		if _, _, err := net.ParseCIDR(sn.CIDR); err != nil {
			allErrs = append(allErrs, field.Invalid(p, sn.CIDR, "must be a valid IPv4/IPv6 CIDR"))
		}
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(c.GroupVersionKind().GroupKind(), c.Name, allErrs)
}
