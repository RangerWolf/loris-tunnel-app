package cloudsync

import (
	"encoding/json"
	"loris-tunnel/internal/conf"
	"os"
	"path/filepath"
)

// Save legacy metadata before scrubbing TOML. Protocol zero prevents treating object hashes as file hashes.
func MigrateBaseline(path string, baseline conf.SyncBaseline) error {
	data, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	var o Options
	if len(data) > 0 {
		if e = json.Unmarshal(data, &o); e != nil {
			return e
		}
	}
	if o.Baseline.SpaceID == "" && baseline.SpaceID != "" {
		// Preserve absent fields: migrating metadata is not a user's enable/disable choice.
		fields := map[string]json.RawMessage{}
		if len(data) > 0 {
			if e := json.Unmarshal(data, &fields); e != nil {
				return e
			}
		}
		if fields == nil {
			fields = make(map[string]json.RawMessage)
		}
		fields["baseline"], e = json.Marshal(baseline)
		if e != nil {
			return e
		}
		encoded, e := json.MarshalIndent(fields, "", "  ")
		if e != nil {
			return e
		}
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		return conf.WritePrivateAtomic(path, encoded)
	}
	return nil
}
