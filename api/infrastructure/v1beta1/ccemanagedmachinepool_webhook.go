/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	"context"
	"regexp"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ValidFlavors is an optional allowlist of ECS flavors accepted by the
// CCEManagedMachinePool webhook. It is populated by the manager from the
// --valid-flavors flag (empty = format check only). Flavors are region- and
// generation-specific and cannot be hard-coded here; region availability is
// still enforced by CCE at create time (error codes such as CCE.01400025 —
// questionnaire Q6/Q7).
var ValidFlavors []string

// flavorPattern matches Huawei Cloud ECS flavor names such as c6.large.2,
// c7.xlarge.4, c6sne.large.2 (family[.variant].size.vcpus).
var flavorPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z0-9]+)?\.[0-9]+(\.[0-9]+)?$`)

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
	} else if !flavorPattern.MatchString(m.Spec.Flavor) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "flavor"), m.Spec.Flavor,
			"flavor must match the ECS flavor naming pattern, e.g. c6.large.2"))
	} else if len(ValidFlavors) > 0 && !slices.Contains(ValidFlavors, m.Spec.Flavor) {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "flavor"), m.Spec.Flavor, ValidFlavors))
	}
	// Official constraint: max 20 taints.
	if len(m.Spec.Taints) > 20 {
		allErrs = append(allErrs, field.TooMany(field.NewPath("spec", "taints"), len(m.Spec.Taints), 20))
	}
	// Official constraint: Turbo >= 1.21 node pools bind max 5 security groups.
	if len(m.Spec.SecurityGroups) > 5 {
		allErrs = append(allErrs, field.TooMany(field.NewPath("spec", "securityGroups"), len(m.Spec.SecurityGroups), 5))
	}
	// Required nodeTemplate fields per the official CreateNodePool API:
	// az ("通过api创建节点不支持随机可用区"), os (required unless a private
	// image is used), rootVolume (size 40-1024 GiB). Fail fast at the API
	// boundary instead of letting the platform reject the request.
	if m.Spec.AvailabilityZone == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "availabilityZone"),
			"availabilityZone is required (CCE does not support random AZ via API)"))
	}
	if m.Spec.OS == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "os"),
			"os is required unless a private image is used, e.g. \"Huawei Cloud EulerOS 2.0\""))
	}
	if m.Spec.RootVolume == nil {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "rootVolume"),
			"rootVolume is required (official size range 40-1024 GiB)"))
	} else if m.Spec.RootVolume.Size < 40 || m.Spec.RootVolume.Size > 1024 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "rootVolume", "size"),
			m.Spec.RootVolume.Size, "root volume size must be within [40, 1024] GiB"))
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(m.GroupVersionKind().GroupKind(), m.Name, allErrs)
}
