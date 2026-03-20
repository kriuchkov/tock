package cli

import (
	"encoding/json"
	"os"

	"github.com/go-faster/errors"
)

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return errors.Wrap(err, "encode json")
	}
	return nil
}
