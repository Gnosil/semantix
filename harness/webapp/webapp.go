// Package webapp assembles the shared loopback HTTP/SSE frontend used by the
// browser command and the native desktop shell.
package webapp

import (
	"semantix/harness/config"
	"semantix/harness/control"
	"semantix/harness/serve"
)

// Assemble wires one controller generation to the canonical Serve protocol.
// The caller owns the controller, broadcaster, listener, and lease lifecycle.
func Assemble(ctrl *control.Controller, bc *serve.Broadcaster, cfg config.ServeConfig, leases *control.SessionLeaseKeeper, listenerAddr string) (*serve.Server, error) {
	srv := serve.New(ctrl, bc, cfg)
	if err := srv.SetSessionLeases(leases); err != nil {
		return nil, err
	}
	srv.EnableProviderSetupForListener(listenerAddr)
	return srv, nil
}
