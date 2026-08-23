/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhookWithManager registers the CCEManagedControlPlane webhook.
func (c *CCEManagedControlPlane) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCEManagedControlPlane{}).
		WithDefaulter(&CCEManagedControlPlane{}).
		WithValidator(&CCEManagedControlPlane{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-controlplane-cluster-x-k8s-io-v1beta2-ccemanagedcontrolplane,mutating=true,failurePolicy=fail,groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes,verbs=create;update,versions=v1beta2,name=mutation.ccemanagedcontrolplane.controlplane.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-controlplane-cluster-x-k8s-io-v1beta2-ccemanagedcontrolplane,mutating=false,failurePolicy=fail,groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes,verbs=create;update,versions=v1beta2,name=validation.ccemanagedcontrolplane.controlplane.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Defaulter[*CCEManagedControlPlane] = &CCEManagedControlPlane{}

// Default implements admission.Defaulter.
func (c *CCEManagedControlPlane) Default(_ context.Context, obj *CCEManagedControlPlane) error {
	if obj.Spec.Category == "" {
		obj.Spec.Category = "Turbo"
	}
	if obj.Spec.ContainerNetwork.Mode == "" {
		obj.Spec.ContainerNetwork.Mode = "eni"
	}
	if obj.Spec.Flavor == "" {
		obj.Spec.Flavor = "cce.s1.small"
	}
	return nil
}

var _ admission.Validator[*CCEManagedControlPlane] = &CCEManagedControlPlane{}

// ValidateCreate implements admission.Validator.
func (c *CCEManagedControlPlane) ValidateCreate(_ context.Context, obj *CCEManagedControlPlane) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (c *CCEManagedControlPlane) ValidateUpdate(_ context.Context, oldObj, newObj *CCEManagedControlPlane) (admission.Warnings, error) {
	var allErrs field.ErrorList
	// Immutable fields: the CCE cluster network config cannot change after
	// creation (official: container network CIDR/mode are immutable). Accepting
	// a change silently would drift spec from the cloud.
	if oldObj.Spec.ContainerNetwork.CIDR != newObj.Spec.ContainerNetwork.CIDR {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "containerNetwork", "cidr"),
			newObj.Spec.ContainerNetwork.CIDR, "field is immutable after creation"))
	}
	if oldObj.Spec.ContainerNetwork.Mode != newObj.Spec.ContainerNetwork.Mode {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "containerNetwork", "mode"),
			newObj.Spec.ContainerNetwork.Mode, "field is immutable after creation"))
	}
	if oldObj.Spec.Category != newObj.Spec.Category {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "category"),
			newObj.Spec.Category, "field is immutable after creation"))
	}
	// Encryption mode and authentication mode are immutable (CCE does not
	// support changing them post-create).
	if oldObj.Spec.EncryptionConfig != nil && newObj.Spec.EncryptionConfig != nil &&
		oldObj.Spec.EncryptionConfig.Mode != newObj.Spec.EncryptionConfig.Mode {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "encryptionConfig", "mode"),
			newObj.Spec.EncryptionConfig.Mode, "field is immutable after creation"))
	}
	if oldObj.Spec.Authentication != nil && newObj.Spec.Authentication != nil &&
		oldObj.Spec.Authentication.Mode != newObj.Spec.Authentication.Mode {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "authentication", "mode"),
			newObj.Spec.Authentication.Mode, "field is immutable after creation"))
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
func (c *CCEManagedControlPlane) ValidateDelete(_ context.Context, _ *CCEManagedControlPlane) (admission.Warnings, error) {
	return nil, nil
}

func (c *CCEManagedControlPlane) validate() error {
	var allErrs field.ErrorList

	if c.Spec.ClusterName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "clusterName"), "clusterName is required"))
	}
	switch c.Spec.Category {
	case "", "CCE", "Turbo":
	default:
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "category"), c.Spec.Category, "must be CCE or Turbo"))
	}
	// eni mode implies Turbo (official SDK comment).
	if c.Spec.ContainerNetwork.Mode == "eni" && c.Spec.Category == "CCE" {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "containerNetwork", "mode"),
			"eni", "eni mode requires category Turbo"))
	}
	// eni mode requires ENI subnets (official: eniNetwork must set subnets or
	// eniSubnetId — our CRD exposes subnets via eniSubnets).
	if c.Spec.ContainerNetwork.Mode == "eni" && len(c.Spec.ContainerNetwork.ENISubnets) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "containerNetwork", "eniSubnets"),
			"eni mode requires at least one ENI subnet (official eniNetwork.subnets)"))
	}
	// Subscription billing (mode=1) requires periodType/periodNum which the
	// CRD does not expose yet — reject it explicitly instead of letting the
	// create loop fail forever on a missing required field.
	if c.Spec.Billing.Mode == 1 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "billing", "mode"), "1",
			"subscription billing is not supported yet (periodType/periodNum not exposed)"))
	}
	// authenticating_proxy mode requires the CA + client cert + key.
	if c.Spec.Authentication != nil && c.Spec.Authentication.Mode == "authenticating_proxy" {
		ap := c.Spec.Authentication.AuthenticatingProxy
		if ap == nil || ap.CA == "" || ap.Cert == "" || ap.PrivateKey == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "authentication", "authenticatingProxy"),
				"authenticating_proxy mode requires ca, cert and privateKey"))
		}
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(c.GroupVersionKind().GroupKind(), c.Name, allErrs)
}
