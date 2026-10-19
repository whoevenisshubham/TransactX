package main

import "testing"

func TestVerificationRejectsForeignOrUnacknowledgedTargets(t *testing.T) {
	const host = "ep-confirmed.neon.tech"
	const dsn = "postgres://fixture:fixture@" + host + "/neondb?sslmode=require"
	const origin = "https://transactx-demo-api.onrender.com"
	const ack = "VERIFY ONE SYNTHETIC GEMINI INVESTIGATION"
	if err := validateTarget(dsn, host, "neondb", origin, ack); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][5]string{
		{dsn, "ep-other.neon.tech", "neondb", origin, ack},
		{dsn, host, "transactx", origin, ack},
		{dsn, host, "neondb", "https://unrelated.invalid", ack},
		{dsn, host, "neondb", origin, ""},
		{"postgres://fixture:fixture@" + host + "/neondb?sslmode=disable", host, "neondb", origin, ack},
	} {
		if validateTarget(c[0], c[1], c[2], c[3], c[4]) == nil {
			t.Fatal("unsafe target accepted")
		}
	}
}
