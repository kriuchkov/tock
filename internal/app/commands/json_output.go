package commands

import (
	"encoding/json"
	"io"
	"os"

	"github.com/go-faster/errors"
)

func writeJSON(value any) error {
	return writeJSONTo(os.Stdout, value)
}

func writeJSONTo(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return errors.Wrap(err, "encode json")
	}
	return nil
}
