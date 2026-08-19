// OPENFRAME(host-assignments): guards the pack-config cache against per-host query targeting
// — openframe/docs/architecture-host-assignments.md
//
// Upstream caches the marshaled pack config per (teamID, queryReportsDisabled) on the premise
// that every host in a team receives the same scheduled queries unless a query uses label
// targeting. In openframe mode that premise is false: ListScheduledQueriesForAgents also filters
// by query_hosts, so the config is host-specific. Serving a team-level cache entry would hand one
// host another host's scheduled queries. This pins the gate so an upstream sync that reworks the
// caching cannot silently reintroduce the leak.
package service

import (
	"context"
	"encoding/json"
	"testing"

	hostctx "github.com/fleetdm/fleet/v4/server/contexts/host"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenframePackConfigCacheIsPerHost(t *testing.T) {
	svc, ds, callCounter := setupPackConfigCacheTest(t)

	// Per-host targeting: each host gets its own scheduled query, as query_hosts would produce.
	ds.ListScheduledQueriesForAgentsFunc = func(ctx context.Context, teamID *uint, hostID *uint, queryReportsDisabled bool) ([]*fleet.Query, error) {
		callCounter.Add(1)
		require.NotNil(t, hostID, "openframe mode must pass the host through to the datastore")
		name := "query_for_host_1"
		if *hostID != 1 {
			name = "query_for_host_2"
		}
		return []*fleet.Query{{Name: name, Query: "SELECT 1", Interval: 60, Logging: "snapshot"}}, nil
	}

	packsFor := func(ctx context.Context) string {
		conf, err := svc.GetClientConfig(ctx)
		require.NoError(t, err)
		raw, ok := conf["packs"].(json.RawMessage)
		require.True(t, ok, "expected a packs section")
		return string(raw)
	}

	host1 := hostctx.NewContext(t.Context(), &fleet.Host{ID: 1})
	host2 := hostctx.NewContext(t.Context(), &fleet.Host{ID: 2})

	t.Run("openframe mode off: team-level cache is reused", func(t *testing.T) {
		_ = packsFor(host1)
		before := callCounter.Load()
		_ = packsFor(host2)
		assert.Equal(t, before, callCounter.Load(),
			"stock Fleet should serve host 2 from the team cache")
	})

	t.Run("openframe mode on: every host is resolved from the datastore", func(t *testing.T) {
		t.Setenv("FLEET_OPENFRAME_MODE", "1")
		require.True(t, fleet.IsOpenframeMode())

		p1 := packsFor(host1)
		before := callCounter.Load()
		p2 := packsFor(host2)

		assert.Greater(t, callCounter.Load(), before,
			"host 2 must not be served from a team-level cache entry")
		assert.Contains(t, p1, "query_for_host_1")
		assert.Contains(t, p2, "query_for_host_2")
		assert.NotContains(t, p2, "query_for_host_1",
			"host 2 received host 1's scheduled queries — the pack-config cache leaked across hosts")
	})
}
