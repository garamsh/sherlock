package sqlite_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// chainStore links mids 1 to 3 as a chain through Gebind Components and
// Mateo Deatreal; mid 4 shares only the office with mid 1, and mid 5 is
// unconnected.
func chainStore(t *testing.T) *sqlite.Store {
	store, _ := linkedStore(t,
		agent.Synthesis{Summary: "Every part for the Rodend build is ordered from Gebind Components, through the office.", Entities: []string{"Rodend", "Gebind Components", "office"}},
		agent.Synthesis{Summary: "Our account at Gebind Components is looked after by Mateo Deatreal.", Entities: []string{"Gebind Components", "Mateo Deatreal"}},
		agent.Synthesis{Summary: "Mateo Deatreal answers on extension 5504.", Entities: []string{"Mateo Deatreal"}},
		agent.Synthesis{Summary: "The office moved to the second floor.", Entities: []string{"office"}},
		agent.Synthesis{Summary: "The harbour ferry runs hourly.", Entities: []string{"harbour"}},
	)
	return store
}

func TestLinked_WalksPastTheSeedsBestFirst(t *testing.T) {
	ctx := context.Background()
	store := chainStore(t)
	linked, err := store.Linked(ctx, "Something is wrong with an order for the Rodend build. What number do I dial?")
	require.NoError(t, err)
	assert.Equal(t, []string{"rodend"}, linked.Seeds)
	assert.Equal(t, []int64{1, 2, 4, 3}, linkedIDs(linked), "the seed's mid first, then one entity away, then two; the unconnected mid is not reached")
	subjects := []string{}
	for _, hit := range linked.Hits {
		subjects = append(subjects, hit.Subject)
	}
	assert.Equal(t, []string{"rodend", "gebind components", "office", "mateo deatreal"}, subjects)

	two, err := store.Linked(ctx, "Who handles Gebind Components for the Rodend build?")
	require.NoError(t, err)
	assert.Equal(t, []string{"gebind components", "rodend"}, two.Seeds, "sorted")

	for _, query := range []string{"What number do I dial?", "Who handles Gebind for the build?"} {
		none, err := store.Linked(ctx, query)
		require.NoError(t, err)
		assert.Empty(t, none.Hits, "the control: %q names no whole stored entity", query)
	}
}
