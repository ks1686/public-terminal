package rebalance

import (
	"testing"
	"time"

	"github.com/ks1686/public-terminal/internal/api"
)

type fakePortfolio struct {
	calls int
	pages []struct {
		p   *api.Portfolio
		err error
	}
}

func (f *fakePortfolio) GetPortfolio() (*api.Portfolio, error) {
	i := f.calls
	if i >= len(f.pages) {
		i = len(f.pages) - 1
	}
	f.calls++
	page := f.pages[i]
	return page.p, page.err
}

func TestWaitForOrdersToClear_ActiveOrdersRemain(t *testing.T) {
	origPoll := OrderPollSecs
	OrderPollSecs = 0
	t.Cleanup(func() { OrderPollSecs = origPoll })

	client := &fakePortfolio{pages: []struct {
		p   *api.Portfolio
		err error
	}{
		{p: &api.Portfolio{Orders: []api.Order{
			{OrderID: "ord-1", Status: "NEW"},
		}}},
	}}

	ok := WaitForOrdersToClear(client, []string{"ord-1"}, "sell", 1)
	if ok {
		t.Fatal("expected wait to fail while the sell is still NEW")
	}
}

func TestWaitForOrdersToClear_ClearsWhenFilled(t *testing.T) {
	origPoll := OrderPollSecs
	OrderPollSecs = 0
	t.Cleanup(func() { OrderPollSecs = origPoll })

	client := &fakePortfolio{pages: []struct {
		p   *api.Portfolio
		err error
	}{
		{p: &api.Portfolio{Orders: []api.Order{
			{OrderID: "ord-1", Status: "NEW"},
		}}},
		{p: &api.Portfolio{Orders: []api.Order{
			{OrderID: "ord-1", Status: "FILLED"},
		}}},
	}}

	ok := WaitForOrdersToClear(client, []string{"ord-1"}, "sell", 2)
	if !ok {
		t.Fatal("expected wait to succeed once the sell is FILLED")
	}
}

func TestWaitForOrdersToClear_MissingIDStillActiveIfAnyOpen(t *testing.T) {
	origPoll := OrderPollSecs
	OrderPollSecs = 0
	t.Cleanup(func() { OrderPollSecs = origPoll })

	client := &fakePortfolio{pages: []struct {
		p   *api.Portfolio
		err error
	}{
		{p: &api.Portfolio{Orders: []api.Order{
			{OrderID: "broker-id", Status: "NEW"},
		}}},
	}}

	ok := WaitForOrdersToClear(client, []string{"client-uuid"}, "sell", 1)
	if ok {
		t.Fatal("client UUID missing from portfolio must not count as filled while any order is still active")
	}
}

func TestSessionDate_UsesNewYork(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	// 11pm PT on Jan 1 is already Jan 2 in New York.
	now := time.Date(2026, 1, 1, 23, 0, 0, 0, loc)
	got := sessionDate(now)
	if got != "2026-01-02" {
		t.Fatalf("sessionDate = %q, want 2026-01-02", got)
	}
}
