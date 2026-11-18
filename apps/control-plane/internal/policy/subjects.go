package policy

import (
	"context"
	"fmt"
	"sync"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// SubjectType is a kind of subject that can wait for approval, registered
// by the capability that owns it, as delivery registers
// delivery.deployment. Its summary type is listed where the API's routes
// are, so the API's description, which is built without wiring, names it.
type SubjectType struct {
	// Type names it, namespaced by the capability, such as delivery.deployment.
	Type string
	// Handler changes the subject when its request is decided.
	Handler SubjectHandler
}

// SubjectHandler changes a waiting subject when a human decides its
// request. Policy calls it inside the transaction that decides the request,
// whose context carries the decision for the subject's ID. A handler error
// rolls the decision back, so the request stays pending.
type SubjectHandler interface {
	// Lock locks the subject until the transaction ends. Policy locks it
	// before the request, as every change to a waiting subject does, so two
	// changes cannot deadlock.
	Lock(ctx context.Context, organizationID, subjectID string) error
	// Approve applies the subject, or refuses when it can no longer be
	// applied.
	Approve(ctx context.Context, actor identity.Actor, r *ApprovalRequest) error
	// Reject marks the subject refused.
	Reject(ctx context.Context, actor identity.Actor, r *ApprovalRequest) error
}

// Subjects is the registry of subject types. Capabilities register theirs
// at startup; policy imports none of them.
type Subjects struct {
	mu    sync.RWMutex
	types map[string]SubjectType
}

func NewSubjects() *Subjects {
	return &Subjects{types: map[string]SubjectType{}}
}

// Register adds a subject type. A type registered twice is an error.
func (s *Subjects) Register(t SubjectType) error {
	if err := (Subject{Type: t.Type, ID: "-"}).validate(); err != nil {
		return err
	}
	if t.Handler == nil {
		return fmt.Errorf("subject type %s needs a handler", t.Type)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.types[t.Type]; ok {
		return fmt.Errorf("subject type %s is registered twice", t.Type)
	}
	s.types[t.Type] = t
	return nil
}

// Lookup returns the registered subject type.
func (s *Subjects) Lookup(subjectType string) (SubjectType, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.types[subjectType]
	return t, ok
}
