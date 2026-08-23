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

// SetupWebhookWithManager registers the CCEClusterTemplate webhook.
func (t *CCEClusterTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCEClusterTemplate{}).
		WithValidator(&CCEClusterTemplate{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta2-cceclustertemplate,mutating=false,failurePolicy=fail,groups=infrastructure.cluster.x-k8s.io,resources=cceclustertemplates,verbs=create;update,versions=v1beta2,name=validation.cceclustertemplate.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Validator[*CCEClusterTemplate] = &CCEClusterTemplate{}

// ValidateCreate implements admission.Validator.
func (t *CCEClusterTemplate) ValidateCreate(_ context.Context, obj *CCEClusterTemplate) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (t *CCEClusterTemplate) ValidateUpdate(_ context.Context, _, newObj *CCEClusterTemplate) (admission.Warnings, error) {
	return nil, newObj.validate()
}

// ValidateDelete implements admission.Validator.
func (t *CCEClusterTemplate) ValidateDelete(_ context.Context, _ *CCEClusterTemplate) (admission.Warnings, error) {
	return nil, nil
}

// validate delegates to the CCECluster validation on the wrapped spec, so a
// bad template is rejected when the ClusterClass is applied rather than when
// the topology controller stamps the cluster.
func (t *CCEClusterTemplate) validate() error {
	c := &CCECluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "CCECluster"},
		ObjectMeta: metav1.ObjectMeta{Name: t.Name},
		Spec:       t.Spec.Template.Spec,
	}
	return c.validate()
}
