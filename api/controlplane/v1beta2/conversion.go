/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

// Hub marks the v1beta2 types as the conversion hub (storage version).
// The v1beta1 types convert to/from these via their ConvertTo/ConvertFrom.
func (*CCEManagedControlPlane) Hub()             {}
func (*CCEManagedControlPlaneList) Hub()         {}
func (*CCEManagedControlPlaneTemplate) Hub()     {}
func (*CCEManagedControlPlaneTemplateList) Hub() {}
