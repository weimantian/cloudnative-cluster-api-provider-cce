/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package cce

import "testing"

func TestParseTaint(t *testing.T) {
	tests := []struct {
		in           string
		wantKey      string
		wantValue    string
		wantEffect   string
	}{
		{in: "dedicated=worker:NoSchedule", wantKey: "dedicated", wantValue: "worker", wantEffect: "NoSchedule"},
		{in: "spot=true:PreferNoSchedule", wantKey: "spot", wantValue: "true", wantEffect: "PreferNoSchedule"},
		{in: "gpu:NoExecute", wantKey: "gpu", wantValue: "", wantEffect: "NoExecute"},
		{in: "plain", wantKey: "plain", wantValue: "", wantEffect: "NoSchedule"},
	}
	for _, tt := range tests {
		key, value, effect := parseTaint(tt.in)
		if key != tt.wantKey || value != tt.wantValue || effect != tt.wantEffect {
			t.Errorf("parseTaint(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tt.in, key, value, effect, tt.wantKey, tt.wantValue, tt.wantEffect)
		}
	}
}

func TestTaintEffectMapping(t *testing.T) {
	noSchedule := taintEffect("NoSchedule")
	if noSchedule.Value() != "NoSchedule" {
		t.Errorf("taintEffect(NoSchedule) = %q, want NoSchedule", noSchedule.Value())
	}
	prefer := taintEffect("PreferNoSchedule")
	if prefer.Value() != "PreferNoSchedule" {
		t.Errorf("taintEffect(PreferNoSchedule) = %q, want PreferNoSchedule", prefer.Value())
	}
}
