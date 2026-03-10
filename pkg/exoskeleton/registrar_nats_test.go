package exoskeleton

import (
	"testing"
)

func TestNATSCredsMapping(t *testing.T) {
	id, err := CompileIdentity("tent-dev", "hn-digest")
	if err != nil {
		t.Fatalf("CompileIdentity returned error: %v", err)
	}

	if id.NATSUser != "tent-dev.hn-digest" {
		t.Errorf("NATSUser = %q, want tent-dev.hn-digest", id.NATSUser)
	}
	if id.NATSPrefix != "tentacular.tent-dev.hn-digest.>" {
		t.Errorf("NATSPrefix = %q, want tentacular.tent-dev.hn-digest.>", id.NATSPrefix)
	}
}

func TestNATSRegistrarClose(t *testing.T) {
	// Close should not panic on a nil registrar fields.
	r := &NATSRegistrar{cfg: NATSConfig{}}
	r.Close()
}
