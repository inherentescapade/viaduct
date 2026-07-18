package cfg

import (
	"testing"
)

// TestUpdateMergesConcurrentWriters covers same-machine pairing: the server and
// a client share one config file, each owning different fields. A blind Save of
// a stale snapshot would erase the other role's fields; Update must merge onto
// whatever is currently on disk so neither is lost.
func TestUpdateMergesConcurrentWriters(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// The server persists an authorized client key (its field).
	if err := Update(func(c *Config) {
		c.AddAuthorizedKey("viaduct1:client-key")
	}); err != nil {
		t.Fatal(err)
	}

	// The client then persists a paired remote (its field), as happens right
	// after the SPAKE2 exchange.
	if err := Update(func(c *Config) {
		c.UpsertRemote(RemoteServer{Name: "localhost:20001", Address: "localhost:20001", PublicKey: "viaduct1:server-key"})
	}); err != nil {
		t.Fatal(err)
	}

	// The server then persists an account token. The server's in-memory config
	// never held the client's remote, so this write must merge with the file
	// rather than overwrite it wholesale.
	if err := Update(func(c *Config) {
		c.SetAccountToken("acct", ClientCredential{Token: "tok", BotMode: false})
	}); err != nil {
		t.Fatal(err)
	}

	got := DefaultConfig()
	if err := got.Load(); err != nil {
		t.Fatal(err)
	}

	// The remote must survive the later server write.
	if r := got.FindRemote("localhost:20001"); r == nil {
		t.Fatal("client remote was clobbered by a later server write")
	} else if r.PublicKey != "viaduct1:server-key" {
		t.Fatalf("remote public key = %q, want viaduct1:server-key", r.PublicKey)
	}
	// And the server's own fields must coexist with it.
	if len(got.Server.AuthorizedKeys) != 1 || got.Server.AuthorizedKeys[0] != "viaduct1:client-key" {
		t.Fatalf("authorized keys = %v, want [viaduct1:client-key]", got.Server.AuthorizedKeys)
	}
	if cred, ok := got.Server.Accounts["acct"]; !ok || cred.Token != "tok" {
		t.Fatalf("account token = %+v, want {tok}", got.Server.Accounts)
	}
}

// TestUpdateCreatesMissingConfig confirms Update starts from defaults when no
// config file exists yet, rather than failing on the missing read.
func TestUpdateCreatesMissingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Update(func(c *Config) {
		c.UpsertRemote(RemoteServer{Name: "vps", Address: "host:21776", PublicKey: "viaduct1:k"})
	}); err != nil {
		t.Fatal(err)
	}

	got := DefaultConfig()
	if err := got.Load(); err != nil {
		t.Fatal(err)
	}
	if got.FindRemote("vps") == nil {
		t.Fatal("remote not saved when starting from a missing config file")
	}
}
