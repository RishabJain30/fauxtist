package wsproto

import (
	"encoding/json"
	"testing"
)

func TestDecodeEnvelopeAndPayload(t *testing.T) {
	raw := `{"type":"stroke","payload":{"points":[{"x":0.5,"y":0.5}],"color":"#000","width":3}}`
	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Type != TypeStroke {
		t.Fatalf("type = %q, want %q", env.Type, TypeStroke)
	}
	var p StrokePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(p.Points) != 1 || p.Points[0].X != 0.5 {
		t.Fatalf("bad points: %+v", p.Points)
	}
}

func TestEncodeServerMessage(t *testing.T) {
	env, err := Encode(TypePhaseChanged, PhaseChangedPayload{Phase: "voting"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"type":"phase_changed","payload":{"phase":"voting"}}` {
		t.Fatalf("unexpected json: %s", b)
	}
}
