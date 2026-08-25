/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Command create-bastion creates a Huawei Cloud ECS bastion host in the
// management VPC: an SSH keypair (private key saved locally), a public
// EulerOS image lookup, a security group with SSH (22) ingress, and a small
// ECS instance with a directly-bound EIP. It prints the public IP and the
// private-key path so the bastion can be reached from the laptop.
//
// Env (CCE_SMOKE_* / CLOUD_SDK_*):
//
//	CLOUD_SDK_AK / CLOUD_SDK_SK   (or CCE_SMOKE_AK/SK)
//	CCE_SMOKE_REGION              (default cn-north-4)
//	CCE_SMOKE_VPC                 management VPC ID (required)
//	CCE_SMOKE_SUBNET              management node subnet ID (required)
//	CCE_SMOKE_AZ                  availability zone (default cn-north-4a)
//	CCE_BASTION_FLAVOR            ECS flavor (default s6.small.1)
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ecsv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2"
	ecsmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2/model"
	ecsRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2/region"
	imsv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ims/v2"
	imsmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ims/v2/model"
	imsRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ims/v2/region"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
)

const (
	bastionName  = "capi-bastion"
	keyName      = "capi-bastion-key"
	sgName       = "capi-bastion-sg"
	keyPath      = "capi-bastion-key.pem"
	waitTimeout  = 10 * time.Minute
	pollInterval = 10 * time.Second
)

func main() {
	ak := envOr("CCE_SMOKE_AK", "CLOUD_SDK_AK")
	sk := envOr("CCE_SMOKE_SK", "CLOUD_SDK_SK")
	region := envDefault("CCE_SMOKE_REGION", "cn-north-4")
	az := envDefault("CCE_SMOKE_AZ", "cn-north-4a")
	vpcID := envOr("CCE_SMOKE_VPC")
	subnetID := envOr("CCE_SMOKE_SUBNET")
	flavor := envDefault("CCE_BASTION_FLAVOR", "s6.small.1")
	if ak == "" || sk == "" {
		fatal("CLOUD_SDK_AK/CCE_SMOKE_AK and CLOUD_SDK_SK/CCE_SMOKE_SK must be set")
	}
	if vpcID == "" || subnetID == "" {
		fatal("CCE_SMOKE_VPC and CCE_SMOKE_SUBNET are required")
	}

	ctx := context.Background()
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	must(err, "build credentials")

	vpcR, err := vpcRegion.SafeValueOf(region)
	must(err, "vpc region")
	vpcHC, err := vpcv2.VpcClientBuilder().WithRegion(vpcR).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "vpc client")
	vpc := vpcv2.NewVpcClient(vpcHC)

	ecsR, err := ecsRegion.SafeValueOf(region)
	must(err, "ecs region")
	ecsHC, err := ecsv2.EcsClientBuilder().WithRegion(ecsR).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "ecs client")
	ecs := ecsv2.NewEcsClient(ecsHC)

	imsR, err := imsRegion.SafeValueOf(region)
	must(err, "ims region")
	imsHC, err := imsv2.ImsClientBuilder().WithRegion(imsR).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "ims client")
	ims := imsv2.NewImsClient(imsHC)

	// 1. Keypair (private key saved locally for SSH).
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		resp, err := ecs.NovaCreateKeypair(&ecsmodel.NovaCreateKeypairRequest{Body: &ecsmodel.NovaCreateKeypairRequestBody{
			Keypair: &ecsmodel.NovaCreateKeypairOption{Name: keyName},
		}})
		must(err, "create keypair")
		if resp.Keypair == nil || resp.Keypair.PrivateKey == "" {
			fatal("create keypair returned no private key")
		}
		if err := os.WriteFile(keyPath, []byte(resp.Keypair.PrivateKey), 0o600); err != nil {
			fatal("write private key: %v", err)
		}
		fmt.Printf("Keypair %s created, private key saved to %s\n", keyName, keyPath)
	} else {
		fmt.Printf("Keypair %s (private key %s exists)\n", keyName, keyPath)
	}

	// 2. Public EulerOS x86_64 image.
	imageID := findImage(ctx, ims)
	if imageID == "" {
		fatal("no public Huawei Cloud EulerOS 2.0 x86_64 image found")
	}
	fmt.Printf("Image: %s\n", imageID)

	// 3. Security group with SSH (22) ingress.
	sgID := findSecurityGroup(ctx, vpc, vpcID, sgName)
	if sgID == "" {
		sgID = createSecurityGroup(ctx, vpc, vpcID, sgName)
		createSSHRule(ctx, vpc, sgID)
	}
	fmt.Printf("Security group: %s\n", sgID)

	// 4. ECS instance with a directly-bound EIP.
	serverID := findBastion(ctx, ecs)
	if serverID == "" {
		serverID = createServer(ctx, ecs, vpcID, subnetID, sgID, imageID, flavor, az)
		fmt.Printf("ECS created: %s\n", serverID)
	} else {
		fmt.Printf("ECS exists: %s\n", serverID)
	}
	waitActive(ctx, ecs, serverID)
	pubIP := publicIP(ctx, ecs, serverID)
	fmt.Printf("\nBASTION_SERVER_ID=%s\nBASTION_PUBLIC_IP=%s\nBASTION_KEY=%s\n", serverID, pubIP, keyPath)
	fmt.Printf("ssh -i %s root@%s\n", keyPath, pubIP)
}

func findImage(ctx context.Context, ims *imsv2.ImsClient) string {
	imagetype := imsmodel.GetListImagesRequestImagetypeEnum().GOLD
	osType := imsmodel.GetListImagesRequestOsTypeEnum().LINUX
	osBit := imsmodel.GetListImagesRequestOsBitEnum().E_64
	status := imsmodel.GetListImagesRequestStatusEnum().ACTIVE
	arch := imsmodel.GetListImagesRequestArchitectureEnum().X86
	req := &imsmodel.ListImagesRequest{
		Imagetype:    &imagetype,
		OsType:       &osType,
		OsBit:        &osBit,
		Status:       &status,
		Architecture: &arch,
		Limit:        int32Ptr(200),
	}
	resp, err := ims.ListImages(req)
	if err != nil || resp.Images == nil {
		return ""
	}
	for _, img := range *resp.Images {
		if img.Name == "" {
			continue
		}
		// x86_64 general-purpose only: skip ARM and bare-metal (Ironic) images.
		if strings.Contains(img.Name, "ARM") || img.VirtualEnvType.Value() == "Ironic" {
			continue
		}
		// Prefer Huawei Cloud EulerOS 2.0.
		if strings.Contains(img.Name, "Huawei Cloud EulerOS") && strings.Contains(img.Name, "2.0") {
			return img.Id
		}
	}
	return ""
}

func findSecurityGroup(ctx context.Context, vpc *vpcv2.VpcClient, vpcID, name string) string {
	resp, err := vpc.ListSecurityGroups(&vpcmodel.ListSecurityGroupsRequest{VpcId: strPtr(vpcID)})
	if err != nil || resp.SecurityGroups == nil {
		return ""
	}
	for _, sg := range *resp.SecurityGroups {
		if sg.Name == name {
			return sg.Id
		}
	}
	return ""
}

func createSecurityGroup(ctx context.Context, vpc *vpcv2.VpcClient, vpcID, name string) string {
	resp, err := vpc.CreateSecurityGroup(&vpcmodel.CreateSecurityGroupRequest{Body: &vpcmodel.CreateSecurityGroupRequestBody{
		SecurityGroup: &vpcmodel.CreateSecurityGroupOption{Name: name, VpcId: strPtr(vpcID)},
	}})
	must(err, "create security group")
	if resp.SecurityGroup == nil || resp.SecurityGroup.Id == "" {
		fatal("create security group returned no id")
	}
	return resp.SecurityGroup.Id
}

func createSSHRule(ctx context.Context, vpc *vpcv2.VpcClient, sgID string) {
	proto := "tcp"
	min := int32(22)
	max := int32(22)
	cidr := "0.0.0.0/0"
	_, err := vpc.CreateSecurityGroupRule(&vpcmodel.CreateSecurityGroupRuleRequest{Body: &vpcmodel.CreateSecurityGroupRuleRequestBody{
		SecurityGroupRule: &vpcmodel.CreateSecurityGroupRuleOption{
			SecurityGroupId: sgID,
			Direction:       "ingress",
			Protocol:        &proto,
			PortRangeMin:    &min,
			PortRangeMax:    &max,
			RemoteIpPrefix:  &cidr,
		},
	}})
	must(err, "create ssh rule")
}

func findBastion(ctx context.Context, ecs *ecsv2.EcsClient) string {
	resp, err := ecs.ListServersDetails(&ecsmodel.ListServersDetailsRequest{Name: strPtr(bastionName)})
	if err != nil || resp.Servers == nil {
		return ""
	}
	for _, s := range *resp.Servers {
		if s.Name == bastionName {
			return s.Id
		}
	}
	return ""
}

func createServer(ctx context.Context, ecs *ecsv2.EcsClient, vpcID, subnetID, sgID, imageID, flavor, az string) string {
	count := int32(1)
	size := int32(40)
	bandSize := int32(1)
	resp, err := ecs.CreateServers(&ecsmodel.CreateServersRequest{Body: &ecsmodel.CreateServersRequestBody{
		Server: &ecsmodel.PrePaidServer{
			ImageRef:  imageID,
			FlavorRef: flavor,
			Name:      bastionName,
			Vpcid:     vpcID,
			Nics: []ecsmodel.PrePaidServerNic{{
				SubnetId: &subnetID,
			}},
			KeyName:  strPtr(keyName),
			Count:    &count,
			RootVolume: &ecsmodel.PrePaidServerRootVolume{
				Volumetype: ecsmodel.GetPrePaidServerRootVolumeVolumetypeEnum().GPSSD,
				Size:       &size,
			},
			SecurityGroups:    &[]ecsmodel.PrePaidServerSecurityGroup{{Id: &sgID}},
			AvailabilityZone:  &az,
			Publicip: &ecsmodel.PrePaidServerPublicip{
				Eip: &ecsmodel.PrePaidServerEip{
					Iptype: "5_bgp",
					Bandwidth: &ecsmodel.PrePaidServerEipBandwidth{
						Sharetype: ecsmodel.GetPrePaidServerEipBandwidthSharetypeEnum().PER,
						Size:      &bandSize,
					},
				},
			},
		},
	}})
	must(err, "create server")
	if resp.ServerIds == nil || len(*resp.ServerIds) == 0 {
		fatal("create server returned no server id")
	}
	return (*resp.ServerIds)[0]
}

func waitActive(ctx context.Context, ecs *ecsv2.EcsClient, serverID string) {
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		resp, err := ecs.ShowServer(&ecsmodel.ShowServerRequest{ServerId: serverID})
		if err == nil && resp.Server != nil {
			fmt.Printf("  server status=%s\n", resp.Server.Status)
			if resp.Server.Status == "ACTIVE" {
				return
			}
			if resp.Server.Status == "ERROR" {
				fatal("server entered ERROR state")
			}
		}
		time.Sleep(pollInterval)
	}
	fatal("server %s did not become ACTIVE within %v", serverID, waitTimeout)
}

func publicIP(ctx context.Context, ecs *ecsv2.EcsClient, serverID string) string {
	resp, err := ecs.ShowServer(&ecsmodel.ShowServerRequest{ServerId: serverID})
	if err != nil || resp.Server == nil {
		return ""
	}
	for _, addrs := range resp.Server.Addresses {
		for _, a := range addrs {
			if a.OSEXTIPStype != nil && a.OSEXTIPStype.Value() == "floating" {
				return a.Addr
			}
		}
	}
	return ""
}

func envOr(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func int32Ptr(i int32) *int32 { return &i }

func must(err error, what string) {
	if err != nil {
		fatal("%s: %v", what, err)
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
