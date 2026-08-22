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

	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
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

func (src *CCECluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCECluster)
	return convertViaJSON(src, dst)
}

func (src *CCECluster) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCECluster)
	return convertViaJSON(dst, src)
}

func (src *CCEClusterList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEClusterList)
	return convertViaJSON(src, dst)
}

func (src *CCEClusterList) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEClusterList)
	return convertViaJSON(dst, src)
}

func (src *CCEManagedMachinePool) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEManagedMachinePool)
	return convertViaJSON(src, dst)
}

func (src *CCEManagedMachinePool) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEManagedMachinePool)
	return convertViaJSON(dst, src)
}

func (src *CCEManagedMachinePoolList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEManagedMachinePoolList)
	return convertViaJSON(src, dst)
}

func (src *CCEManagedMachinePoolList) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEManagedMachinePoolList)
	return convertViaJSON(dst, src)
}

func (src *CCEClusterControllerIdentity) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEClusterControllerIdentity)
	return convertViaJSON(src, dst)
}

func (src *CCEClusterControllerIdentity) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEClusterControllerIdentity)
	return convertViaJSON(dst, src)
}

func (src *CCEClusterControllerIdentityList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEClusterControllerIdentityList)
	return convertViaJSON(src, dst)
}

func (src *CCEClusterControllerIdentityList) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEClusterControllerIdentityList)
	return convertViaJSON(dst, src)
}

func (src *CCEClusterStaticIdentity) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEClusterStaticIdentity)
	return convertViaJSON(src, dst)
}

func (src *CCEClusterStaticIdentity) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEClusterStaticIdentity)
	return convertViaJSON(dst, src)
}

func (src *CCEClusterStaticIdentityList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEClusterStaticIdentityList)
	return convertViaJSON(src, dst)
}

func (src *CCEClusterStaticIdentityList) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEClusterStaticIdentityList)
	return convertViaJSON(dst, src)
}

func (src *CCEClusterRoleIdentity) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEClusterRoleIdentity)
	return convertViaJSON(src, dst)
}

func (src *CCEClusterRoleIdentity) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEClusterRoleIdentity)
	return convertViaJSON(dst, src)
}

func (src *CCEClusterRoleIdentityList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEClusterRoleIdentityList)
	return convertViaJSON(src, dst)
}

func (src *CCEClusterRoleIdentityList) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEClusterRoleIdentityList)
	return convertViaJSON(dst, src)
}

func (src *CCEClusterTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEClusterTemplate)
	return convertViaJSON(src, dst)
}

func (src *CCEClusterTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEClusterTemplate)
	return convertViaJSON(dst, src)
}

func (src *CCEClusterTemplateList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEClusterTemplateList)
	return convertViaJSON(src, dst)
}

func (src *CCEClusterTemplateList) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEClusterTemplateList)
	return convertViaJSON(dst, src)
}

func (src *CCEManagedMachinePoolTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEManagedMachinePoolTemplate)
	return convertViaJSON(src, dst)
}

func (src *CCEManagedMachinePoolTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEManagedMachinePoolTemplate)
	return convertViaJSON(dst, src)
}

func (src *CCEManagedMachinePoolTemplateList) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1beta2.CCEManagedMachinePoolTemplateList)
	return convertViaJSON(src, dst)
}

func (src *CCEManagedMachinePoolTemplateList) ConvertFrom(srcRaw conversion.Hub) error {
	dst := srcRaw.(*infrav1beta2.CCEManagedMachinePoolTemplateList)
	return convertViaJSON(dst, src)
}
