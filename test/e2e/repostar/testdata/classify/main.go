// Command classify is the e2e stand-in for a GitHub star classifier step
// (ADR-058, additive ops): a repository whose data.starred is not true
// gets the star-candidate tag and a metadata.star record; a starred one
// is left alone. It never echoes tags, so the tags earlier steps set
// must survive it.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type object struct {
	Metadata map[string]any `json:"metadata"`
}

type op struct {
	Op    string `json:"op"`
	Value any    `json:"value"`
}

func main() {
	confidence := 0.9
	if p := os.Getenv("DPKMS_STEP_CONFIG_PATH"); p != "" {
		b, err := os.ReadFile(filepath.Clean(p)) // #nosec G304 G703 -- the config file dpkms names
		if err == nil {
			var cfg struct {
				Confidence float64 `json:"confidence"`
			}
			if json.Unmarshal(b, &cfg) == nil && cfg.Confidence > 0 {
				confidence = cfg.Confidence
			}
		}
	}
	var o object
	if err := json.NewDecoder(os.Stdin).Decode(&o); err != nil {
		fmt.Println(`{"error":{"code":"BAD_INPUT","message":"input is not a knowledge object"}}`)
		os.Exit(1)
	}
	data, _ := o.Metadata["data"].(map[string]any)
	ops := []op{}
	if starred, _ := data["starred"].(bool); !starred {
		ops = append(ops,
			op{"add_tags", []any{map[string]any{"label": "star-candidate", "weight": confidence, "source": "classify"}}},
			op{"merge_metadata", map[string]any{"star": map[string]any{"confidence": confidence, "lists": data["star_lists"]}}},
		)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"ops": ops}); err != nil {
		os.Exit(2)
	}
}
