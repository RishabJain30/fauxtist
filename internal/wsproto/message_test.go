package wsproto

import (
	"encoding/json"
	"testing"
)

func TestDecodeCommandEnvelope(t *testing.T) {
	raw := `{"version":2,"type":"set_orders","requestId":"r1","payload":{"faux":true,"commands":[{"type":"march","from":"A","to":"B","armies":2}]}}`
	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Type != TypeSetOrders {
		t.Fatalf("type = %q, want %q", env.Type, TypeSetOrders)
	}
	if env.Version != ProtocolVersion {
		t.Fatalf("version = %d, want %d", env.Version, ProtocolVersion)
	}
	var p SetOrdersPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !p.Faux || len(p.Commands) != 1 || p.Commands[0].Type != "march" || p.Commands[0].Armies != 2 {
		t.Fatalf("bad payload: %+v", p)
	}
}

func TestEncodeServerMessage(t *testing.T) {
	env, err := Encode(TypeError, ErrorPayload{Message: "nope", Code: "invalid_orders"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"version":2,"type":"error","payload":{"message":"nope","code":"invalid_orders"}}`
	if string(b) != want {
		t.Fatalf("unexpected json:\n got %s\nwant %s", b, want)
	}
}

func TestValidateEnvelopeRejectsUnknownAndRequiresRequestID(t *testing.T) {
	// Unknown type.
	if err := ValidateEnvelope(Envelope{Type: "not_a_real_type", RequestID: "r"}); err == nil {
		t.Fatal("expected an error for an unknown type")
	}
	// Missing request id.
	if err := ValidateEnvelope(Envelope{Type: TypeLockOrders}); err == nil {
		t.Fatal("expected an error for a missing request id")
	}
	// Valid.
	if err := ValidateEnvelope(Envelope{Type: TypeLockOrders, RequestID: "r"}); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}
}
