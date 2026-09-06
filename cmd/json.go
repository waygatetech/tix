package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
)

var fieldsOutput string

func printJSON(value any, compact bool, fields string) error {
	if fields != "" {
		filtered, err := filterJSONFields(value, fields)
		if err != nil {
			return err
		}
		value = filtered
	}

	var (
		out []byte
		err error
	)
	if compact {
		out, err = json.Marshal(value)
	} else {
		out, err = json.MarshalIndent(value, "", "  ")
	}
	if err != nil {
		return fmt.Errorf("json failed: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func filterJSONFields(value any, fields string) (any, error) {
	wanted := map[string]bool{}
	for _, field := range strings.Split(fields, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			return nil, fmt.Errorf("empty field in --fields")
		}
		wanted[field] = true
	}

	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("json failed: %w", err)
	}

	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("json failed: %w", err)
	}

	switch typed := decoded.(type) {
	case map[string]any:
		return filterMap(typed, wanted)
	case []any:
		filtered := make([]any, 0, len(typed))
		for _, item := range typed {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("--fields requires object JSON output")
			}
			filteredItem, err := filterMap(m, wanted)
			if err != nil {
				return nil, err
			}
			filtered = append(filtered, filteredItem)
		}
		return filtered, nil
	default:
		return nil, fmt.Errorf("--fields requires object JSON output")
	}
}

func filterMap(values map[string]any, wanted map[string]bool) (map[string]any, error) {
	filtered := map[string]any{}
	for field := range wanted {
		value, ok := values[field]
		if !ok {
			return nil, fmt.Errorf("unknown field: %s", field)
		}
		filtered[field] = value
	}
	return filtered, nil
}
