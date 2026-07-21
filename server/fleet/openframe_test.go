package fleet

import (
	"context"
	"testing"
)

func TestParseOpenframeTeamID(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		wantID uint
		wantOK bool
	}{
		{name: "blank", raw: "", wantID: 0, wantOK: false},
		{name: "valid", raw: "5", wantID: 5, wantOK: true},
		{name: "zero is not a valid team", raw: "0", wantID: 0, wantOK: false},
		{name: "non-numeric", raw: "abc", wantID: 0, wantOK: false},
		{name: "negative", raw: "-1", wantID: 0, wantOK: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotID, gotOK := parseOpenframeTeamID(c.raw)
			if gotID != c.wantID || gotOK != c.wantOK {
				t.Fatalf("parseOpenframeTeamID(%q) = (%d, %v), want (%d, %v)", c.raw, gotID, gotOK, c.wantID, c.wantOK)
			}
		})
	}
}

func TestParseOpenframeEnabled(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{" true ", true},
		{"false", false},
		{"0", false},
		{"yes", false},
		{"enabled", false},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			if got := parseOpenframeEnabled(c.raw); got != c.want {
				t.Fatalf("parseOpenframeEnabled(%q) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

func TestOpenframeTeamIDFromEnvDecision(t *testing.T) {
	// Flag off ⇒ the env pin is inert (upstream behavior, even with a stray team id set).
	if _, ok := openframeTeamIDFromEnvDecision(false, "7"); ok {
		t.Fatal("flag off: FLEET_OPENFRAME_TEAM_ID must not pin the process")
	}
	// Flag on ⇒ the env pin applies.
	if id, ok := openframeTeamIDFromEnvDecision(true, "7"); !ok || id != 7 {
		t.Fatalf("flag on: env pin should apply, got (%d,%v)", id, ok)
	}
	// Flag on with no/bad value ⇒ no pin (shared mode; bad values are caught at startup by
	// ValidateOpenframeMultitenancy).
	if _, ok := openframeTeamIDFromEnvDecision(true, ""); ok {
		t.Fatal("flag on with empty team id must not pin")
	}
}

func TestOpenframeTeamIDContextOverridesEnv(t *testing.T) {
	// The context value must win regardless of the env-derived (cached) pin — and it must work
	// even with the multitenancy flag off, because tests and the (flag-gated) middleware are the
	// only writers of the ctx value.
	ctx := NewOpenframeTeamContext(context.Background(), 9)
	id, ok := OpenframeTeamID(ctx)
	if !ok || id != 9 {
		t.Fatalf("context team should be used: got (%d, %v), want (9, true)", id, ok)
	}
}

func TestOpenframeSharedModeDecision(t *testing.T) {
	cases := []struct {
		name                             string
		multitenancy, hasUUID, hasTeamID bool
		want                             bool
	}{
		{name: "off is never shared", multitenancy: false, want: false},
		{name: "off with uuid is never shared", multitenancy: false, hasUUID: true, want: false},
		{name: "on + uuid = pinned", multitenancy: true, hasUUID: true, want: false},
		{name: "on + team id = pinned", multitenancy: true, hasTeamID: true, want: false},
		{name: "on + no pin = shared", multitenancy: true, want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := openframeSharedModeDecision(c.multitenancy, c.hasUUID, c.hasTeamID); got != c.want {
				t.Fatalf("openframeSharedModeDecision(%v,%v,%v) = %v, want %v",
					c.multitenancy, c.hasUUID, c.hasTeamID, got, c.want)
			}
		})
	}
}

func TestOpenframeMultitenancyConfigError(t *testing.T) {
	cases := []struct {
		name          string
		multitenancy  bool
		teamIDRaw     string
		tenantUUIDRaw string
		err           bool
	}{
		{name: "off is always nil", multitenancy: false, err: false},
		{name: "off ignores a bad team id", multitenancy: false, teamIDRaw: "abc", err: false},
		{name: "off ignores a bad tenant uuid", multitenancy: false, tenantUUIDRaw: "nope", err: false},
		{name: "on + no pin = shared mode, valid", multitenancy: true, err: false},
		{name: "on + valid team id = pinned, valid", multitenancy: true, teamIDRaw: "5", err: false},
		{name: "on + unparsable team id errors", multitenancy: true, teamIDRaw: "abc", err: true},
		{name: "on + zero team id errors", multitenancy: true, teamIDRaw: "0", err: true},
		{name: "on + valid tenant uuid = pinned, valid", multitenancy: true, tenantUUIDRaw: "1877e27c-b3fa-488f-82b6-449b80c1cc97", err: false},
		{name: "on + valid tenant uuid with surrounding space, valid", multitenancy: true, tenantUUIDRaw: "  1877e27c-b3fa-488f-82b6-449b80c1cc97  ", err: false},
		{name: "on + malformed tenant uuid errors", multitenancy: true, tenantUUIDRaw: "openframe-junk", err: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotErr := openframeMultitenancyConfigError(c.multitenancy, c.teamIDRaw, c.tenantUUIDRaw) != nil
			if gotErr != c.err {
				t.Fatalf("openframeMultitenancyConfigError(%v,%q,%q) err=%v, want %v",
					c.multitenancy, c.teamIDRaw, c.tenantUUIDRaw, gotErr, c.err)
			}
		})
	}
}

func TestValidateOpenframeMultitenancy(t *testing.T) {
	// Off → no-op (the pre-migration per-tenant-database phase, where there is no team pin).
	t.Setenv("FLEET_OPENFRAME_MULTI_TENANCY_ENABLED", "")
	t.Setenv("FLEET_OPENFRAME_TEAM_ID", "")
	if err := ValidateOpenframeMultitenancy(); err != nil {
		t.Fatalf("multitenancy off must be a no-op: got %v", err)
	}

	// On with no pin → shared per-request mode, valid.
	t.Setenv("FLEET_OPENFRAME_MULTI_TENANCY_ENABLED", "true")
	if err := ValidateOpenframeMultitenancy(); err != nil {
		t.Fatalf("multitenancy on with no pin is shared mode and must validate: got %v", err)
	}

	// On with an unparsable team id → refuse to boot (silent fallback into the wrong mode).
	t.Setenv("FLEET_OPENFRAME_TEAM_ID", "not-a-team")
	if err := ValidateOpenframeMultitenancy(); err == nil {
		t.Fatal("multitenancy on with an unparsable FLEET_OPENFRAME_TEAM_ID must fail")
	}

	// On with a valid team id → pinned mode, valid.
	t.Setenv("FLEET_OPENFRAME_TEAM_ID", "5")
	if err := ValidateOpenframeMultitenancy(); err != nil {
		t.Fatalf("multitenancy on with a valid team id must validate: got %v", err)
	}
}

func TestOpenframeTenantUUID(t *testing.T) {
	t.Setenv("FLEET_OPENFRAME_TENANT_UUID", "  ")
	if _, ok := OpenframeTenantUUID(); ok {
		t.Fatal("blank/whitespace UUID must be treated as unset")
	}
	t.Setenv("FLEET_OPENFRAME_TENANT_UUID", "3f1a9b2c-0000-4d5e-8f00-000000000001")
	u, ok := OpenframeTenantUUID()
	if !ok || u != "3f1a9b2c-0000-4d5e-8f00-000000000001" {
		t.Fatalf("OpenframeTenantUUID() = (%q,%v), want the set UUID", u, ok)
	}
}

func TestSetOpenframeTeamIDPins(t *testing.T) {
	// Resolved pin takes precedence over the env fallback and is returned by OpenframeTeamID.
	// (The atomic pin is deliberately not gated on the flag inside OpenframeTeamID: its only
	// production writer, cmd/fleet/serve.go, is itself flag-gated.)
	openframePinnedTeamID.Store(0)
	t.Cleanup(func() { openframePinnedTeamID.Store(0) })

	SetOpenframeTeamID(0) // ignored
	if v := openframePinnedTeamID.Load(); v != 0 {
		t.Fatalf("zero pin must be ignored, got %d", v)
	}
	SetOpenframeTeamID(7)
	id, ok := OpenframeTeamID(context.Background())
	if !ok || id != 7 {
		t.Fatalf("resolved pin should be returned: got (%d,%v), want (7,true)", id, ok)
	}
}
