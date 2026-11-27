package snmp

import (
	"testing"
	"time"
)

// gosnmp's ExponentialTimeout doubles the wait on every retry: 75 s for one
// unanswered request at 5 s x 3 retries. It must stay off, and the default
// timeout is the Datadog agent's 2 s.
func TestSessionTimeoutSettings(t *testing.T) {
	sess, err := NewSession(ConnectionConfig{
		Host:        "127.0.0.1",
		Credentials: Credentials{Version: "v2c", Community: "public"},
		Retries:     3,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := sess.(*gosnmpSession).client
	if client.ExponentialTimeout {
		t.Error("ExponentialTimeout must be off")
	}
	if client.Timeout != 2*time.Second {
		t.Errorf("default timeout = %s, want 2s", client.Timeout)
	}
	if worst := client.Timeout * time.Duration(client.Retries+1); worst > 10*time.Second {
		t.Errorf("one unanswered request costs %s", worst)
	}
}
