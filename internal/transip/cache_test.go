package transip

import (
	"testing"
	"time"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

// clock is a time source that only moves when told to
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func cachingProvider(repo *fakeRepository, duration time.Duration) (*Provider, *clock) {
	c := &clock{t: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	p := NewWithRepository(repo, nil, false)
	p.domainCacheDuration = duration
	p.now = c.now
	return p, c
}

// Every sync reads the records and applies the changes, and both need the
// zones. The account has a budget of 1000 TransIP API calls an hour, shared
// with everything else that uses it, so the domains are listed once per cache
// duration, not twice per sync.
func TestDomainsAreListedOncePerCacheDuration(t *testing.T) {
	repo := newFakeRepository("example.org")
	p, c := cachingProvider(repo, time.Hour)

	web := endpoint.NewEndpoint("www.example.org", "A", "192.0.2.1").
		WithLabel(endpoint.ResourceLabelKey, "service/default/web")

	for range 10 {
		syncOnce(t, p, &plan.SyncPolicy{}, web)
		c.t = c.t.Add(time.Minute)
	}
	if repo.listed != 1 {
		t.Errorf("expected the domains to be listed once, got %d", repo.listed)
	}

	c.t = c.t.Add(time.Hour)
	syncOnce(t, p, &plan.SyncPolicy{}, web)
	if repo.listed != 2 {
		t.Errorf("expected the domains to be listed again after the cache duration, got %d", repo.listed)
	}
}

func TestNoDomainCacheListsTheDomainsOnEveryCall(t *testing.T) {
	repo := newFakeRepository("example.org")
	p, _ := cachingProvider(repo, 0)

	for range 3 {
		if _, err := p.Records(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if repo.listed != 3 {
		t.Errorf("expected the domains to be listed 3 times, got %d", repo.listed)
	}
}

// A domain that leaves the account is noticed on the next sync, not after the
// cache duration.
func TestADomainThatLeftTheAccountIsForgotten(t *testing.T) {
	repo := newFakeRepository("example.org")
	repo.zones["example.net"] = nil
	p, _ := cachingProvider(repo, time.Hour)

	if _, err := p.Records(t.Context()); err != nil {
		t.Fatal(err)
	}

	delete(repo.zones, "example.net")
	if _, err := p.Records(t.Context()); err == nil {
		t.Fatal("expected an error for the domain that left the account")
	}
	if _, err := p.Records(t.Context()); err != nil {
		t.Fatalf("expected the domains to be listed again, got %v", err)
	}
	if repo.listed != 2 {
		t.Errorf("expected the domains to be listed twice, got %d", repo.listed)
	}
}
