package fleet

import "os"

// IsOpenframeMode returns true when FLEET_OPENFRAME_MODE=1 is set.
// All hosts_include_any (policy_hosts / query_hosts) logic is gated behind this flag.
func IsOpenframeMode() bool {
	return os.Getenv("FLEET_OPENFRAME_MODE") == "1"
}
