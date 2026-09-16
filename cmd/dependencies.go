package cmd

import "github.com/waygatetech/tix/internal/tix"

func resolveRefs(refs []string) ([]int, error) {
	ids := make([]int, 0, len(refs))
	for _, ref := range refs {
		id, err := tix.ResolveRef(ref)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
