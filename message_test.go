package main

import "testing"

func TestNewMessageSetsPriorityFromType(t *testing.T) {
	cases := []struct {
		typ  MessageType
		want Priority
	}{
		{TypeEmergency, Emergency},
		{TypeSickness, CheckIn}, // health reports go with check-ins, ahead of chatter
		{TypeCheckIn, CheckIn},
		{TypeGeneral, General},
	}
	for _, tc := range cases {
		m := NewMessage("m1", "A", "B", tc.typ, "body")
		if m.Priority != tc.want {
			t.Errorf("%s: priority = %d, want %d", tc.typ, m.Priority, tc.want)
		}
		if m.Type != tc.typ {
			t.Errorf("Type = %q, want %q", m.Type, tc.typ)
		}
	}
}
