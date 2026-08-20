/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	"encoding/json"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
)

// convertViaJSON round-trips the source through JSON into dst. The v1beta1
// and v1beta2 types are structurally identical (v1beta1 already implements the
// v1beta2 contract via metav1.Condition), so a JSON round-trip is a complete,
// lossless conversion.
func convertViaJSON(src, dst interface{}) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func (src *CCEManagedControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*controlplanev1beta2.CCEManagedControlPlane)
	return convertViaJSON(src, dst)
}

func (src *CCEManagedControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*controlplanev1beta2.CCEManagedControlPlane)
	return convertViaJSON(dst, src)
}

func (src *CCEManagedControlPlaneList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*controlplanev1beta2.CCEManagedControlPlaneList)
	return convertViaJSON(src, dst)
}

func (src *CCEManagedControlPlaneList) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*controlplanev1beta2.CCEManagedControlPlaneList)
	return convertViaJSON(dst, src)
}

func (src *CCEManagedControlPlaneTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*controlplanev1beta2.CCEManagedControlPlaneTemplate)
	return convertViaJSON(src, dst)
}

func (src *CCEManagedControlPlaneTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*controlplanev1beta2.CCEManagedControlPlaneTemplate)
	return convertViaJSON(dst, src)
}

func (src *CCEManagedControlPlaneTemplateList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*controlplanev1beta2.CCEManagedControlPlaneTemplateList)
	return convertViaJSON(src, dst)
}

func (src *CCEManagedControlPlaneTemplateList) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*controlplanev1beta2.CCEManagedControlPlaneTemplateList)
	return convertViaJSON(dst, src)
}
