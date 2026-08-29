package sources

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

const maximumConfigurationBytes = 2 << 20

func LoadRegistry(path string) (Registry, error) {
	var registry Registry
	if err := decodeStrictYAML(path, &registry); err != nil {
		return Registry{}, fmt.Errorf("load source registry: %w", err)
	}
	return registry, nil
}

func LoadFixtureCatalog(path string) (FixtureCatalog, error) {
	var catalog FixtureCatalog
	if err := decodeStrictYAML(path, &catalog); err != nil {
		return FixtureCatalog{}, fmt.Errorf("load fixture catalog: %w", err)
	}
	return catalog, nil
}

func decodeStrictYAML(path string, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	information, err := file.Stat()
	if err != nil {
		return err
	}
	if information.Size() > maximumConfigurationBytes {
		return fmt.Errorf("configuration exceeds %d bytes", maximumConfigurationBytes)
	}

	limited := io.LimitReader(file, maximumConfigurationBytes+1)
	decoder := yaml.NewDecoder(limited)
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		return err
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple YAML documents are not allowed")
		}
		return err
	}

	return nil
}
