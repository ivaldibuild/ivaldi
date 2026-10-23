package aggregate

import (
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// ErrInvalidID is returned for a stored ID that does not fit its type.
var ErrInvalidID = errors.New("invalid ID")

// Entity is a domain object's identity. A domain object embeds it, through
// Root for an aggregate or directly for a child record an aggregate owns, so
// the object never generates its own ID.
type Entity struct {
	id    string
	isNew bool
}

// NewEntity returns the identity of a domain object whose IDs carry prefix:
// storedID when a repository rebuilds a stored object, which must be a valid
// ID with that prefix, or a new ID otherwise. An empty prefix means plain
// ULIDs. A new ID panics if prefix was never registered with pkg/id, which is
// a programming error.
func NewEntity(prefix string, storedID *string) (Entity, error) {
	if storedID == nil {
		if prefix == "" {
			return Entity{id: id.ULID(), isNew: true}, nil
		}
		return Entity{id: id.New(prefix), isNew: true}, nil
	}
	if prefix == "" {
		if !id.ValidULID(*storedID) {
			return Entity{}, fmt.Errorf("%w: %q is not a ULID", ErrInvalidID, *storedID)
		}
	} else if !id.Valid(prefix, *storedID) {
		return Entity{}, fmt.Errorf("%w: %q is not a %s_ ID", ErrInvalidID, *storedID, prefix)
	}
	return Entity{id: *storedID, isNew: false}, nil
}

// ID returns the object's ID.
func (e Entity) ID() string {
	return e.id
}

// IsNew reports whether the object was created rather than rebuilt from a
// stored ID. It stays true after the object is saved, so a constructor that
// also rebuilds stored objects uses it to record a creation event only once.
func (e Entity) IsNew() bool {
	return e.isNew
}
