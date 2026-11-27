package cmd

import "github.com/waygatetech/tix/internal/tix"

func resolveRefs(refs []string) ([]string, error) {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		id, err := tix.ResolveRef(ref)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
