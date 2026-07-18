package main

import (
	"fmt"

	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/engine"
	"github.com/inherentescapade/viaduct/server"
)

// This file exposes "local monitors": retention policies that run in-process
// while the desktop app is open, with no server required. They reuse the same
// MonitorManager the hosted server uses, backed by the locally-detected token.

// initLocalMonitors constructs the in-process monitor manager. The engine is
// built from the app's current credentials each run, so re-authenticating with a
// new token is picked up automatically.
func (a *App) initLocalMonitors() {
	build := func(c server.Credentials) (*engine.Engine, error) {
		if c.Token == "" {
			return nil, fmt.Errorf("no Discord token; sign in on the Live tab first")
		}
		return engine.New(c.Token, c.BotMode), nil
	}
	creds := func() server.Credentials {
		a.mu.Lock()
		defer a.mu.Unlock()
		return server.Credentials{Token: a.token, BotMode: a.botMode}
	}
	a.localMon = server.NewMonitorManager(cfg.LocalMonitorsPath(), build, creds, nil)
}

// LocalMonitors lists the in-process monitor policies.
func (a *App) LocalMonitors() ([]MonitorDTO, error) {
	out := make([]MonitorDTO, 0)
	for _, m := range a.localMon.List() {
		out = append(out, toMonitorDTO(m))
	}
	return out, nil
}

// SetLocalMonitor creates or updates an in-process monitor policy.
func (a *App) SetLocalMonitor(req MonitorReq) (*MonitorDTO, error) {
	m, err := a.localMon.Upsert(req.toPolicy())
	if err != nil {
		return nil, err
	}
	d := toMonitorDTO(m)
	return &d, nil
}

// DeleteLocalMonitor removes an in-process monitor policy.
func (a *App) DeleteLocalMonitor(id string) error {
	if !a.localMon.Delete(id) {
		return fmt.Errorf("monitor %q not found", id)
	}
	return nil
}

// PreviewLocalMonitor reports how many messages a local monitor would delete now.
func (a *App) PreviewLocalMonitor(req MonitorReq) (*PreviewDTO, error) {
	n, err := a.localMon.Preview(req.toPolicy())
	if err != nil {
		return nil, err
	}
	return &PreviewDTO{Target: req.Name, Total: n}, nil
}
