package aggregate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
)

type widgetRenamed struct {
	Name string
}

func (widgetRenamed) EventName() string { return "test.widget.renamed.v1" }

type widgetArchived struct{}

func (widgetArchived) EventName() string { return "test.widget.archived.v1" }

type widget struct {
	aggregate.Root
	name string
}

func (w *widget) Rename(name string) {
	w.name = name
	w.RecordEvent(widgetRenamed{Name: name})
}

func (w *widget) Archive() {
	w.RecordEvent(widgetArchived{})
}

func TestPullEventsReturnsEventsInRecordedOrder(t *testing.T) {
	w := &widget{}
	w.Rename("gear")
	w.Archive()

	assert.Equal(t, []aggregate.Event{widgetRenamed{Name: "gear"}, widgetArchived{}}, w.PullEvents())
}

func TestPullEventsForgetsPulledEvents(t *testing.T) {
	w := &widget{}
	w.Rename("gear")
	w.PullEvents()

	assert.Empty(t, w.PullEvents(), "pulled events are saved once")

	w.Archive()
	assert.Equal(t, []aggregate.Event{widgetArchived{}}, w.PullEvents(), "events recorded after a pull are kept")
}

func TestZeroRootHasNoEvents(t *testing.T) {
	var r aggregate.Root

	assert.Empty(t, r.PullEvents())
}
