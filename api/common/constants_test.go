/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package common

import "testing"

func TestTagLengthConstants(t *testing.T) {
	if MaxTagKeyLength != 128 {
		t.Errorf("MaxTagKeyLength = %d, want 128", MaxTagKeyLength)
	}
	if MaxTagValueLength != 255 {
		t.Errorf("MaxTagValueLength = %d, want 255", MaxTagValueLength)
	}
}

func TestContainerModeConstants(t *testing.T) {
	if ModeENI != "eni" {
		t.Errorf("ModeENI = %q, want \"eni\"", ModeENI)
	}
	if ModeVPCRouter != "vpc-router" {
		t.Errorf("ModeVPCRouter = %q, want \"vpc-router\"", ModeVPCRouter)
	}
	if ModeOverlayL2 != "overlay_l2" {
		t.Errorf("ModeOverlayL2 = %q, want \"overlay_l2\"", ModeOverlayL2)
	}
}

func TestDefaultFlavorAndEIPTypeConstants(t *testing.T) {
	if DefaultFlavor != "cce.s1.small" {
		t.Errorf("DefaultFlavor = %q, want \"cce.s1.small\"", DefaultFlavor)
	}
	if DefaultEIPType != "5_bgp" {
		t.Errorf("DefaultEIPType = %q, want \"5_bgp\"", DefaultEIPType)
	}
}
