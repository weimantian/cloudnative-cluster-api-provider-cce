/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package credentials

import "testing"

func TestNewProvider(t *testing.T) {
	p := NewProvider()
	if p == nil {
		t.Fatal("NewProvider() returned nil")
	}
	// The STS-backed provider must be the concrete implementation with the
	// function seams tests override (mirrors the existing provider tests).
	if _, ok := p.(*provider); !ok {
		t.Errorf("NewProvider() = %T, want *provider", p)
	}
}
