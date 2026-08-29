package extraction

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/traweezy/relantern/contracts/schemas"
)

func StructuredOutputSchema() (map[string]any, error) {
	var schema map[string]any
	if err := json.Unmarshal(schemas.StructuredExtractionV1(), &schema); err != nil {
		return nil, fmt.Errorf("decode structured extraction schema: %w", err)
	}
	return schema, nil
}

func SchemaDigest() [sha256.Size]byte {
	return sha256.Sum256(schemas.StructuredExtractionV1())
}
