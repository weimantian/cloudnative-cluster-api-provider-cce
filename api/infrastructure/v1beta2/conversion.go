/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

// Hub marks the v1beta2 types as the conversion hub (storage version).
// The v1beta1 types convert to/from these via their ConvertTo/ConvertFrom.
func (*CCECluster) Hub()                        {}
func (*CCEClusterList) Hub()                    {}
func (*CCEManagedMachinePool) Hub()             {}
func (*CCEManagedMachinePoolList) Hub()         {}
func (*CCEClusterControllerIdentity) Hub()      {}
func (*CCEClusterControllerIdentityList) Hub()  {}
func (*CCEClusterStaticIdentity) Hub()          {}
func (*CCEClusterStaticIdentityList) Hub()      {}
func (*CCEClusterRoleIdentity) Hub()            {}
func (*CCEClusterRoleIdentityList) Hub()        {}
func (*CCEClusterTemplate) Hub()                {}
func (*CCEClusterTemplateList) Hub()            {}
func (*CCEManagedMachinePoolTemplate) Hub()     {}
func (*CCEManagedMachinePoolTemplateList) Hub() {}
