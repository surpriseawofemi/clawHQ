package protocol

import (
	"encoding/json"
	"testing"
)

// TestMarshalWithZeroValues tests marshaling protocol types with zero values.
func TestMarshalWithZeroValues(t *testing.T) {
	tests := []struct {
		name string
		val  any
	}{
		{"empty request", Request{}},
		{"empty response", Response{}},
		{"empty event", Event{}},
		{"empty connect params", ConnectParams{}},
		{"empty client info", ClientInfo{}},
		{"empty hello ok", HelloOK{}},
		{"empty presence entry", PresenceEntry{}},
		{"empty chat send params", ChatSendParams{}},
		{"empty agent params", AgentParams{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.val)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if len(data) == 0 {
				t.Error("marshaled to empty")
			}
		})
	}
}

// TestUnmarshalWithUnknownFields tests that unknown JSON fields don't break unmarshaling.
func TestUnmarshalWithUnknownFields(t *testing.T) {
	tests := []struct {
		name string
		json string
		into any
	}{
		{
			"request with unknown field",
			`{"type":"req","id":"1","method":"test","params":{},"unknownField":"value"}`,
			&Request{},
		},
		{
			"response with unknown field",
			`{"type":"res","id":"1","ok":true,"newField":123}`,
			&Response{},
		},
		{
			"event with unknown field",
			`{"type":"event","event":"test","payload":{},"extra":"data"}`,
			&Event{},
		},
		{
			"connect params with future field",
			`{"minProtocol":3,"maxProtocol":3,"client":{},"futureFeature":true}`,
			&ConnectParams{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tt.json), tt.into); err != nil {
				t.Errorf("unmarshal with unknown fields: %v", err)
			}
		})
	}
}

// TestMarshalResponseWithNilPayload tests marshaling a response with nil payload.
func TestMarshalResponseWithNilPayload(t *testing.T) {
	data, err := MarshalResponse("test-1", nil)
	if err != nil {
		t.Fatalf("MarshalResponse with nil: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Error("ok = false")
	}
	if resp.ID != "test-1" {
		t.Errorf("id = %q", resp.ID)
	}
}

// TestMarshalEventWithNilPayload tests marshaling an event with nil payload.
func TestMarshalEventWithNilPayload(t *testing.T) {
	data, err := MarshalEvent("test.event", nil)
	if err != nil {
		t.Fatalf("MarshalEvent with nil: %v", err)
	}

	var ev Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if ev.EventName != "test.event" {
		t.Errorf("eventName = %q", ev.EventName)
	}
}

// TestEmptyErrorPayload tests error payload with empty values.
func TestEmptyErrorPayload(t *testing.T) {
	ep := ErrorPayload{}
	data, err := json.Marshal(ep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ErrorPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Code != "" {
		t.Errorf("code = %q, want empty", got.Code)
	}
}

// TestConnectParamsWithEmptyAuth tests ConnectParams with empty auth.
func TestConnectParamsWithEmptyAuth(t *testing.T) {
	params := ConnectParams{
		MinProtocol: 3,
		MaxProtocol: 3,
		Client:      ClientInfo{ID: "test"},
		Auth:        AuthParams{}, // Empty auth
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ConnectParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Auth.Token != "" || got.Auth.Password != "" {
		t.Error("empty auth should remain empty")
	}
}

// TestPresenceEntryWithAllOptionalFields tests PresenceEntry with all optional fields.
func TestPresenceEntryWithAllOptionalFields(t *testing.T) {
	lastInput := 42
	pe := PresenceEntry{
		Ts:               1234567890,
		DeviceID:         "dev-1",
		Host:             "host-1",
		IP:               "10.0.0.1",
		Version:          "1.0.0",
		Platform:         "linux",
		DeviceFamily:     "desktop",
		ModelIdentifier:  "model-x",
		Mode:             "operator",
		LastInputSeconds: &lastInput,
		Reason:           "active",
		Tags:             []string{"primary", "main"},
		Text:             "working",
		Roles:            []string{"operator"},
		Scopes:           []string{"operator.read", "operator.write"},
		InstanceID:       "inst-1",
	}

	data, err := json.Marshal(pe)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got PresenceEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.DeviceID != "dev-1" {
		t.Errorf("deviceId mismatch")
	}
	if got.LastInputSeconds == nil || *got.LastInputSeconds != 42 {
		t.Error("lastInputSeconds mismatch")
	}
	if len(got.Tags) != 2 {
		t.Errorf("tags len = %d", len(got.Tags))
	}
	if len(got.Roles) != 1 {
		t.Errorf("roles len = %d", len(got.Roles))
	}
	if len(got.Scopes) != 2 {
		t.Errorf("scopes len = %d", len(got.Scopes))
	}
}

// TestParseFrameWithInvalidJSON tests ParseFrame with completely invalid JSON.
func TestParseFrameWithInvalidJSON(t *testing.T) {
	inputs := []string{
		"",
		"not json at all",
		"{incomplete",
		"[]",
		"123",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := ParseFrame([]byte(input))
			if err == nil {
				t.Error("expected error for invalid JSON")
			}
		})
	}
}

// TestParseFrameWithMissingTypeField tests ParseFrame when type field is missing.
func TestParseFrameWithMissingTypeField(t *testing.T) {
	input := `{"id":"1","method":"test"}`
	frame, err := ParseFrame([]byte(input))
	if err != nil {
		t.Fatalf("ParseFrame: %v", err)
	}

	// Should parse but have empty type
	if frame.Type != "" {
		t.Errorf("type = %q, expected empty", frame.Type)
	}
}

// TestParseFrameWithNonStringType tests ParseFrame when type is not a string.
func TestParseFrameWithNonStringType(t *testing.T) {
	input := `{"type":123}`
	if _, err := ParseFrame([]byte(input)); err == nil {
		t.Fatal("expected error for non-string type")
	}
}

// TestUnmarshalRequestWithMissingFields tests UnmarshalRequest with minimal data.
func TestUnmarshalRequestWithMissingFields(t *testing.T) {
	input := `{"type":"req"}`
	req, err := UnmarshalRequest([]byte(input))
	if err != nil {
		t.Fatalf("UnmarshalRequest: %v", err)
	}

	if req.Type != FrameTypeRequest {
		t.Errorf("type = %q", req.Type)
	}
	if req.ID != "" {
		t.Errorf("id = %q, expected empty", req.ID)
	}
	if req.Method != "" {
		t.Errorf("method = %q, expected empty", req.Method)
	}
}

// TestExecFinishedWithNilPointers tests ExecFinished with nil optional pointers.
func TestExecFinishedWithNilPointers(t *testing.T) {
	ef := ExecFinished{
		SessionKey: "main",
		RunID:      "run-1",
		Command:    "ls",
		ExitCode:   nil,
		TimedOut:   nil,
		Success:    nil,
		Output:     "output text",
	}

	data, err := json.Marshal(ef)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ExecFinished
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ExitCode != nil {
		t.Error("exitCode should be nil")
	}
	if got.TimedOut != nil {
		t.Error("timedOut should be nil")
	}
	if got.Success != nil {
		t.Error("success should be nil")
	}
}

// TestSessionDefaultsWithAllFields tests SessionDefaults roundtrip.
func TestSessionDefaultsWithAllFields(t *testing.T) {
	sd := SessionDefaults{
		DefaultAgentID: "default-agent",
		MainKey:        "main",
		MainSessionKey: "main-session",
		Scope:          "per-sender",
	}

	data, err := json.Marshal(sd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got SessionDefaults
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.DefaultAgentID != "default-agent" {
		t.Errorf("defaultAgentID = %q", got.DefaultAgentID)
	}
	if got.Scope != "per-sender" {
		t.Errorf("scope = %q", got.Scope)
	}
}

// TestHealthEventWithNilChannels tests HealthEvent with nil/empty channels.
func TestHealthEventWithNilChannels(t *testing.T) {
	ev := HealthEvent{
		OK:             true,
		Ts:             1234,
		DurationMs:     50,
		Channels:       nil,
		ChannelOrder:   []string{},
		ChannelLabels:  map[string]string{},
		DefaultAgentID: "main",
		Agents:         []AgentHealthSummary{},
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got HealthEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !got.OK {
		t.Error("ok = false")
	}
}

// TestChatEventMessageStringPayload ensures string message payloads round-trip.
func TestChatEventMessageStringPayload(t *testing.T) {
	input := `{"runId":"run-1","sessionKey":"main","seq":1,"state":"final","message":"hello","usage":{"promptTokens":1}}`
	var ev ChatEvent
	if err := json.Unmarshal([]byte(input), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var msg string
	if err := json.Unmarshal(ev.Message, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if msg != "hello" {
		t.Errorf("message = %q", msg)
	}
	var usage map[string]any
	if err := json.Unmarshal(ev.Usage, &usage); err != nil {
		t.Fatalf("unmarshal usage: %v", err)
	}
	if usage["promptTokens"] != float64(1) {
		t.Errorf("usage.promptTokens = %v", usage["promptTokens"])
	}
}

// TestChatEventMessageObjectPayload ensures object message payloads round-trip.
func TestChatEventMessageObjectPayload(t *testing.T) {
	input := `{"runId":"run-2","sessionKey":"main","seq":2,"state":"delta","message":{"role":"assistant","content":"hi"}}`
	var ev ChatEvent
	if err := json.Unmarshal([]byte(input), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var msg map[string]string
	if err := json.Unmarshal(ev.Message, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if msg["content"] != "hi" {
		t.Errorf("content = %q", msg["content"])
	}
}
