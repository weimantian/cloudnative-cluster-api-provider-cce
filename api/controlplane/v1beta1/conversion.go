/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
)

// convertViaJSON round-trips the source through JSON into dst. The v1beta1
// and v1beta2 types are structurally identical (v1beta1 already implements the
// v1beta2 contract via metav1.Condition), so a JSON round-trip is a complete,
// lossless conversion.
func convertViaJSON(src, dst interface{}) error {
	// The conversion webhook allocates dst with the target version's
	// apiVersion/kind already set (see allocateDstObject in controller-runtime).
	// A naive JSON round-trip would overwrite dst's TypeMeta with the source's
	// apiVersion/kind, so capture and restore the destination GVK around the
	// round-trip to keep the returned object at the requested version.
	dstObj, dstIsObject := dst.(runtime.Object)
	var dstGVK schema.GroupVersionKind
	if dstIsObject {
		dstGVK = dstObj.GetObjectKind().GroupVersionKind()
	}
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return err
	}
	if dstIsObject && !dstGVK.Empty() {
		dstObj.GetObjectKind().SetGroupVersionKind(dstGVK)
	}
	return nil
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
