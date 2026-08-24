/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package scope

import "github.com/pkg/errors"

// GlobalScope is the account-wide context for non-cluster-scoped controllers
// (e.g. the GC orphan sweeper). Mirrors CAPA's GlobalScope but simplified:
// CCE does not cache a long-lived AWS-style session — SDK clients are
// per-region+credentials and cached in the reconciler-level sync.Map. The
// GlobalScope only carries identifying metadata (region, controller name).
//
// CAPA's full GlobalScope (aws.Config, throttle.ServiceLimiters) is not
// modeled here because CCE's GC operates through the same Reconciler
// factory pattern as the regular controllers.
type GlobalScope struct {
	region         string
	controllerName string
}

// GlobalScopeParams is the input for NewGlobalScope.
type GlobalScopeParams struct {
	Region         string
	ControllerName string
}

// NewGlobalScope builds a new global scope.
func NewGlobalScope(params GlobalScopeParams) (*GlobalScope, error) {
	if params.Region == "" {
		return nil, errors.New("region is required")
	}
	if params.ControllerName == "" {
		return nil, errors.New("controllerName is required")
	}
	return &GlobalScope{
		region:         params.Region,
		controllerName: params.ControllerName,
	}, nil
}

// Region returns the cloud region this scope covers.
func (s *GlobalScope) Region() string { return s.region }

// ControllerName returns the controller name (used as cache key prefix).
func (s *GlobalScope) ControllerName() string { return s.controllerName }
