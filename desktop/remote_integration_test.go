package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/server"
)

// TestDesktopRemoteBindingsEndToEnd drives the desktop App's self-hosting
// bindings against a real in-process server over HTTP + ECIES, exercising
// identity creation, saving the remote, and a ping round trip.
func TestDesktopRemoteBindingsEndToEnd(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	app := NewApp()

	// 1. The wizard's "your key" step.
	idDTO, err := app.EnsureIdentity()
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if !strings.HasPrefix(idDTO.PublicKey, "viaduct1:") {
		t.Fatalf("unexpected public key form: %q", idDTO.PublicKey)
	}

	// 2. Stand up a server with no authorized clients, exactly as `viaduct serve`
	// does. It shows a code only when a client requests pairing (captured here).
	serverID, _ := auth.GenerateIdentity()
	var shownCode string
	srv, err := server.New(server.Options{
		Identity:      serverID,
		MonitorsPath:  t.TempDir() + "/monitors.bin",
		OnPairingCode: func(code string, _ time.Time, _ string) { shownCode = code },
		Logf:          func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	// 3. The wizard's pairing step: request a code (shown on the server), then
	// enter it. No keys copied.
	if err := app.RemotePairRequest(addr); err != nil {
		t.Fatalf("RemotePairRequest: %v", err)
	}
	if shownCode == "" {
		t.Fatal("requesting pairing should have shown a code on the server")
	}
	if _, err := app.RemotePairComplete(addr, shownCode); err != nil {
		t.Fatalf("RemotePairComplete: %v", err)
	}
	if r, _ := app.GetRemote(); r == nil || r.Address != addr {
		t.Fatalf("GetRemote did not return the paired remote")
	}

	// 4. The wizard's "test connection" step — pairing authorized us live.
	ping, err := app.RemotePing()
	if err != nil {
		t.Fatalf("RemotePing: %v", err)
	}
	if ping.Version != server.Version {
		t.Fatalf("unexpected version %q", ping.Version)
	}
	if ping.HasToken {
		t.Fatal("fresh server should report no token")
	}

	// 5. Monitors and jobs should be listable (empty) without a token.
	if mons, err := app.RemoteMonitors(); err != nil || len(mons) != 0 {
		t.Fatalf("RemoteMonitors: got %d err %v", len(mons), err)
	}
	if jobs, err := app.RemoteJobs(); err != nil || len(jobs) != 0 {
		t.Fatalf("RemoteJobs: got %d err %v", len(jobs), err)
	}

	// 6. Forget clears it.
	if err := app.ForgetRemote(); err != nil {
		t.Fatalf("ForgetRemote: %v", err)
	}
	if r, _ := app.GetRemote(); r != nil {
		t.Fatal("remote should be gone after ForgetRemote")
	}
}
