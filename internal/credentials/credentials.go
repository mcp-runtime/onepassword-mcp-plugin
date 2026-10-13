// Package credentials looks up a verified user's credentials with a bounded
// in-memory cache.
//
// The cache is consulted only after mcp-auth authorised the request, so a hit
// can never bypass a session-revocation check. Entries are keyed by verified
// issuer, subject, service and mapping version, never outlive the OAuth
// session or the configured TTL, and expired entries are never served, not
// even when the provider is down.
package credentials

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/headers"
	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/mappings"
	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/secrets"
)

// ErrNoMapping means the verified identity has no approved mapping for the service.
var ErrNoMapping = errors.New("no credential mapping")

// Identity is a verified (issuer, subject) pair.
type Identity struct{ Issuer, Subject string }

// Injected is the set of headers to add to an upstream request. Its printed
// form shows header names only.
type Injected struct{ values map[string]string }

// Values returns the header values. Callers must not log them.
func (i Injected) Values() map[string]string { return i.values }

func (i Injected) String() string {
	names := make([]string, 0, len(i.values))
	for n := range i.values {
		names = append(names, n)
	}
	return fmt.Sprintf("Injected(headers=%v)", names)
}
func (i Injected) GoString() string { return i.String() }

type cacheKey struct{ issuer, subject, service, version string }

type entry struct {
	key     cacheKey
	value   Injected
	expires time.Time
}

// Service resolves and caches credentials.
type Service struct {
	Mappings mappings.Store
	Resolver secrets.Resolver
	TTL      time.Duration
	Max      int
	Now      func() time.Time

	mu    sync.Mutex
	items map[cacheKey]*list.Element
	order *list.List
}

// HeadersFor returns the headers for identity/service. sessionExpiry bounds
// the cache lifetime so a cached credential cannot outlive the OAuth session.
func (s *Service) HeadersFor(ctx context.Context, id Identity, service string, specs []headers.Spec, sessionExpiry time.Time) (Injected, error) {
	mapping, err := s.Mappings.Lookup(id.Issuer, id.Subject, service)
	if err != nil {
		return Injected{}, err
	}
	if mapping == nil {
		return Injected{}, ErrNoMapping
	}
	key := cacheKey{id.Issuer, id.Subject, service, mapping.Version}
	now := s.now()
	if hit, ok := s.get(key, now); ok {
		return hit, nil
	}
	refs := map[string]string{}
	for _, field := range headers.RequiredFields(specs) {
		ref, ok := mapping.Refs[field]
		if !ok {
			return Injected{}, secrets.ErrFieldUnavailable
		}
		refs[field] = ref
	}
	values, err := s.Resolver.Resolve(ctx, refs)
	if err != nil {
		return Injected{}, err
	}
	rendered, err := headers.RenderAll(specs, values)
	if err != nil {
		return Injected{}, secrets.ErrFieldUnavailable
	}
	injected := Injected{values: rendered}
	expires := now.Add(s.TTL)
	if sessionExpiry.Before(expires) {
		expires = sessionExpiry
	}
	if expires.After(now) {
		s.put(key, injected, expires)
	}
	return injected, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) get(key cacheKey, now time.Time) (Injected, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	element, ok := s.items[key]
	if !ok {
		return Injected{}, false
	}
	e := element.Value.(*entry)
	if !e.expires.After(now) {
		delete(s.items, key)
		s.order.Remove(element)
		return Injected{}, false
	}
	s.order.MoveToFront(element)
	return e.value, true
}

func (s *Service) put(key cacheKey, value Injected, expires time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items, s.order = map[cacheKey]*list.Element{}, list.New()
	}
	if element, ok := s.items[key]; ok {
		element.Value = &entry{key, value, expires}
		s.order.MoveToFront(element)
	} else {
		s.items[key] = s.order.PushFront(&entry{key, value, expires})
	}
	limit := s.Max
	if limit < 1 {
		limit = 1
	}
	for len(s.items) > limit {
		oldest := s.order.Back()
		delete(s.items, oldest.Value.(*entry).key)
		s.order.Remove(oldest)
	}
}

// Len reports the number of cached entries.
func (s *Service) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
