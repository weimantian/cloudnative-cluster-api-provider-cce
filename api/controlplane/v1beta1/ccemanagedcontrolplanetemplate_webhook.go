/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhookWithManager registers the CCEManagedControlPlaneTemplate webhook.
func (t *CCEManagedControlPlaneTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCEManagedControlPlaneTemplate{}).
		WithDefaulter(&CCEManagedControlPlaneTemplate{}).
		WithValidator(&CCEManagedControlPlaneTemplate{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-controlplane-cluster-x-k8s-io-v1beta1-ccemanagedcontrolplanetemplate,mutating=true,failurePolicy=fail,groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanetemplates,verbs=create;update,versions=v1beta1,name=mutation.ccemanagedcontrolplanetemplate.controlplane.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-controlplane-cluster-x-k8s-io-v1beta1-ccemanagedcontrolplanetemplate,mutating=false,failurePolicy=fail,groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanetemplates,verbs=create;update,versions=v1beta1,name=validation.ccemanagedcontrolplanetemplate.controlplane.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Defaulter[*CCEManagedControlPlaneTemplate] = &CCEManagedControlPlaneTemplate{}

// Default implements admission.Defaulter.
func (t *CCEManagedControlPlaneTemplate) Default(_ context.Context, obj *CCEManagedControlPlaneTemplate) error {
	if obj.Spec.Template.Spec.Category == "" {
		obj.Spec.Template.Spec.Category = "Turbo"
	}
	if obj.Spec.Template.Spec.ContainerNetwork.Mode == "" {
		obj.Spec.Template.Spec.ContainerNetwork.Mode = "eni"
	}
	if obj.Spec.Template.Spec.Flavor == "" {
		obj.Spec.Template.Spec.Flavor = "cce.s1.small"
	}
	return nil
}

var _ admission.Validator[*CCEManagedControlPlaneTemplate] = &CCEManagedControlPlaneTemplate{}

// ValidateCreate implements admission.Validator.
func (t *CCEManagedControlPlaneTemplate) ValidateCreate(_ context.Context, obj *CCEManagedControlPlaneTemplate) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (t *CCEManagedControlPlaneTemplate) ValidateUpdate(_ context.Context, _, newObj *CCEManagedControlPlaneTemplate) (admission.Warnings, error) {
	return nil, newObj.validate()
}

// ValidateDelete implements admission.Validator.
func (t *CCEManagedControlPlaneTemplate) ValidateDelete(_ context.Context, _ *CCEManagedControlPlaneTemplate) (admission.Warnings, error) {
	return nil, nil
}

// validate delegates to the CCEManagedControlPlane validation on the wrapped
// spec, so a bad template is rejected when the ClusterClass is applied.
func (t *CCEManagedControlPlaneTemplate) validate() error {
	c := &CCEManagedControlPlane{
		TypeMeta:   metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "CCEManagedControlPlane"},
		ObjectMeta: metav1.ObjectMeta{Name: t.Name},
		Spec:       t.Spec.Template.Spec,
	}
	// clusterName is filled by the topology controller from a patch (builtin
	// {{ .clusterName }}), so it is legitimately empty on the template;
	// supply a placeholder to bypass only that one required-field check.
	if c.Spec.ClusterName == "" {
		c.Spec.ClusterName = t.Name
	}
	return c.validate()
}
