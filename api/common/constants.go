/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package common

// Tag key/value length limits (official Huawei Cloud ResourceTag / TMS docs).
const (
	// MaxTagKeyLength is the maximum tag key length in characters.
	MaxTagKeyLength = 128
	// MaxTagValueLength is the maximum tag value length in characters.
	MaxTagValueLength = 255
)

// Container network modes (official CCE enum: overlay_l2 | vpc-router | eni).
const (
	// ModeENI is the Turbo (eni) container network mode.
	ModeENI = "eni"
	// ModeVPCRouter is the Standard vpc-router container network mode.
	ModeVPCRouter = "vpc-router"
	// ModeOverlayL2 is the Standard overlay_l2 container network mode.
	ModeOverlayL2 = "overlay_l2"
)

// Default cluster flavor and EIP type.
const (
	// DefaultFlavor is the CCE cluster flavor applied when unset.
	DefaultFlavor = "cce.s1.small"
	// DefaultEIPType is the Huawei Cloud BGP public IP type.
	DefaultEIPType = "5_bgp"
)
