// OPENFRAME(waf-inventory-shape): guards the query side of the DN hex-encoding
// — openframe/docs/agent-inventory-waf-shape.md
//
// The ingest side (decodeCertificateDNColumns) is covered in queries_test.go;
// this pins the SQL itself. An upstream sync that takes upstream's certificate
// detail queries would drop the hex() wrapping with no git conflict, and every
// inventory write would start tripping the edge WAF's SQLi rules again.
package osquery_utils

import (
	"fmt"
	"testing"

	"github.com/fleetdm/fleet/v4/server/config"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

func TestOpenframeCertificateQueriesHexEncodeDN(t *testing.T) {
	queries := GetDetailQueries(t.Context(), config.FleetConfig{}, nil, &fleet.Features{}, Integrations{}, nil)

	// Each platform must hex-encode exactly the DN columns its ingest decodes, so these are
	// keyed off the production column lists rather than repeating them: Windows reads osquery's
	// subject2/issuer2 (which preserve the DN attribute keys), macOS reads subject/issuer.
	for name, dnCols := range map[string][]string{
		"certificates_darwin":  certificateDNColumns,
		"certificates_windows": certificateDNColumnsWindows,
	} {
		q, ok := queries[name]
		require.True(t, ok, "detail query %s is missing", name)

		for _, col := range dnCols {
			require.Contains(t, q.Query, fmt.Sprintf("hex(%s) AS %s_hex", col, col),
				"%s must hex-encode its DN columns for the WAF", name)
			// The raw column must not leak through alongside the hex one.
			require.NotRegexp(t, `(?m)^\s*`+col+`\s*,`, q.Query,
				"%s selects raw DN column %s", name, col)
		}
	}
}
