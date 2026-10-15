package tracker

import (
	"reflect"
	"testing"
	"time"

	"github.com/ogredforman/ServerDog/internal/source"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

const every = time.Minute // snapshot RefreshEvery used throughout

func testOptions() Options {
	return Options{StartingWindow: 20 * time.Minute, DropPct: 30, DropMin: 25}
}

func srv(id string, players int) source.Server {
	return source.Server{
		ID:         id,
		InstanceID: "inst-1",
		Players:    players,
		MaxPlayers: 128,
		Map:        source.Map{Variant: "map-1", Base: "base"},
		Mode:       source.Mode{Experience: "exp-1"},
	}
}

func snap(id string, at time.Time, servers ...source.Server) source.Snapshot {
	return source.Snapshot{ID: id, FetchedAt: at, RefreshEvery: every, Complete: true, Servers: servers}
}

func exact(at time.Time) source.MatchStart {
	return source.MatchStart{State: source.Exact, At: at}
}

var notStarted = source.MatchStart{State: source.NotStarted}

func mustGet(t *testing.T, tr *Tracker, id string) Server {
	t.Helper()
	s, ok := tr.Get(id)
	if !ok {
		t.Fatalf("server %q not tracked", id)
	}
	return s
}

func queueIDs(tr *Tracker) []string {
	var ids []string
	for _, d := range tr.Queue() {
		ids = append(ids, d.ID)
	}
	return ids
}

// TestObserveTiers covers SPEC §7.2 rules 1–4, 6 and 8 for one server across two snapshots.
func TestObserveTiers(t *testing.T) {
	ms := exact(t0.Add(-5 * time.Minute))
	with := func(s source.Server, f func(*source.Server)) source.Server { f(&s); return s }

	tests := []struct {
		name       string
		prev       *source.Server // nil: the server is new in the second snapshot
		cur        source.Server
		wantTier   Tier
		wantChange bool
	}{
		{"new empty server is never queued", nil, srv("a", 0), None, false},
		{"known empty server is never queued", ptr(srv("a", 50)), srv("a", 0), None, false},
		{"empty server with a change is never queued", ptr(srv("a", 50)), with(srv("a", 0), func(s *source.Server) { s.InstanceID = "inst-2" }), None, false},
		{"new server", nil, srv("a", 50), P2, false},
		{"new server with provider start", nil, with(srv("a", 50), func(s *source.Server) { s.MatchStart = &ms }), None, false},
		{"changed server with provider start", ptr(srv("a", 50)), with(srv("a", 50), func(s *source.Server) { s.InstanceID = "inst-2"; s.MatchStart = &ms }), None, false},
		{"instance changed", ptr(srv("a", 50)), with(srv("a", 50), func(s *source.Server) { s.InstanceID = "inst-2" }), P0, true},
		{"map variant changed", ptr(srv("a", 50)), with(srv("a", 50), func(s *source.Server) { s.Map.Variant = "map-2" }), P0, true},
		{"experience changed", ptr(srv("a", 50)), with(srv("a", 50), func(s *source.Server) { s.Mode.Experience = "exp-2" }), P0, true},
		{"map base or name change alone is not a change", ptr(srv("a", 50)), with(srv("a", 50), func(s *source.Server) { s.Map.Base = "other"; s.Name = "x" }), P3, false},
		{"unchanged server", ptr(srv("a", 50)), srv("a", 50), P3, false},
		{"more players", ptr(srv("a", 50)), srv("a", 120), P3, false},
		{"drop at both thresholds", ptr(srv("a", 100)), srv("a", 70), P0, false},
		{"drop below percent", ptr(srv("a", 100)), srv("a", 71), P3, false},
		{"drop below minimum", ptr(srv("a", 80)), srv("a", 56), P3, false},
		{"large drop", ptr(srv("a", 60)), srv("a", 34), P0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := New(testOptions())
			if tc.prev != nil {
				tr.Observe(snap("s1", t0, *tc.prev))
			}
			t1 := t0.Add(every)
			tr.Observe(snap("s2", t1, tc.cur))

			got := mustGet(t, tr, "a")
			if got.Tier != tc.wantTier {
				t.Errorf("tier = %v, want %v", got.Tier, tc.wantTier)
			}
			if tc.wantChange != got.ChangeObservedAt.Equal(t1) {
				t.Errorf("ChangeObservedAt = %v, want set to %v: %v", got.ChangeObservedAt, t1, tc.wantChange)
			}
			if !tc.wantChange && !got.ChangeObservedAt.IsZero() {
				t.Errorf("ChangeObservedAt = %v, want zero", got.ChangeObservedAt)
			}
			if tc.cur.MatchStart != nil && got.Start != *tc.cur.MatchStart {
				t.Errorf("Start = %v, want provider start %v", got.Start, *tc.cur.MatchStart)
			}
			inQueue := len(tr.Queue()) > 0
			if inQueue != (tc.wantTier != None) {
				t.Errorf("queued = %v, want %v", inQueue, tc.wantTier != None)
			}
		})
	}
}

func ptr(s source.Server) *source.Server { return &s }

func TestDropMinZeroLeavesOnlyPercent(t *testing.T) {
	o := testOptions()
	o.DropMin = 0
	tr := New(o)
	tr.Observe(snap("s1", t0, srv("a", 10)))
	tr.Observe(snap("s2", t0.Add(every), srv("a", 7)))
	if got := mustGet(t, tr, "a").Tier; got != P0 {
		t.Errorf("tier = %v, want P0", got)
	}
}

func TestEmptyServerResetsStart(t *testing.T) {
	tr := New(testOptions())
	tr.Observe(snap("s1", t0, srv("a", 50)))
	tr.Apply("a", exact(t0.Add(-time.Minute)), t0)
	changed := srv("a", 50)
	changed.InstanceID = "inst-2"
	tr.Observe(snap("s2", t0.Add(every), changed))
	tr.Observe(snap("s3", t0.Add(2*every), srv("a", 0)))

	got := mustGet(t, tr, "a")
	if got.Start != (source.MatchStart{}) || !got.ChangeObservedAt.IsZero() {
		t.Errorf("after emptying: Start = %v, ChangeObservedAt = %v; want unknown and zero", got.Start, got.ChangeObservedAt)
	}
	if got.InstanceID != "inst-1" || got.Players != 0 {
		t.Errorf("state not recorded: instance %q players %d", got.InstanceID, got.Players)
	}
	if p, _ := tr.Published("a", t0.Add(2*every)); p.State != Unknown {
		t.Errorf("published = %v, want unknown", p.State)
	}
}

func TestServersWithoutJoinCodeAreNotTracked(t *testing.T) {
	tr := New(testOptions())
	tr.Observe(snap("s1", t0, srv("", 50), srv("a", 50), srv("a", 10)))
	if q := queueIDs(tr); !reflect.DeepEqual(q, []string{"a"}) {
		t.Errorf("queue = %v, want [a]", q)
	}
	if got := mustGet(t, tr, "a").Players; got != 50 {
		t.Errorf("duplicate join code: players = %d, want first entry's 50", got)
	}
}

// TestStartingWindow covers SPEC §7.2 rule 5: P1 while waiting inside StartingWindow, then P3.
func TestStartingWindow(t *testing.T) {
	tests := []struct {
		name  string
		apply *source.MatchStart // result applied in the change snapshot; nil: P0 went unchecked
		after time.Duration      // time of the next snapshot after the change
		want  Tier
	}{
		{"not-started inside window", &notStarted, 10 * time.Minute, P1},
		{"not-started at window end", &notStarted, 20 * time.Minute, P1},
		{"not-started after window", &notStarted, 20*time.Minute + time.Second, P3},
		{"old exact start inside window", ptrMS(exact(t0.Add(-time.Hour))), 5 * time.Minute, P1},
		{"old exact start after window", ptrMS(exact(t0.Add(-time.Hour))), 25 * time.Minute, P3},
		{"confirmed exact start", ptrMS(exact(t0.Add(every))), 5 * time.Minute, P3},
		{"unchecked change inside window", nil, 5 * time.Minute, P1},
		{"unchecked change after window", nil, 21 * time.Minute, P3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := New(testOptions())
			tr.Observe(snap("s1", t0, srv("a", 50)))
			changed := srv("a", 50)
			changed.InstanceID = "inst-2"
			tc1 := t0.Add(every)
			tr.Observe(snap("s2", tc1, changed))
			if tc.apply != nil {
				tr.Apply("a", *tc.apply, tc1.Add(time.Second))
			}
			tr.Observe(snap("s3", tc1.Add(tc.after), changed))
			if got := mustGet(t, tr, "a").Tier; got != tc.want {
				t.Errorf("tier = %v, want %v", got, tc.want)
			}
		})
	}
}

func ptrMS(m source.MatchStart) *source.MatchStart { return &m }

// TestApply covers SPEC §7.3, including the snapshot-rounding acceptance window, and the
// published state that follows.
func TestApply(t *testing.T) {
	tc := t0.Add(every) // ChangeObservedAt
	inside := tc.Add(2 * time.Minute)
	outside := tc.Add(21 * time.Minute)

	tests := []struct {
		name       string
		result     source.MatchStart
		checkedAt  time.Time
		wantTier   Tier
		wantChange bool // ChangeObservedAt still set (unconfirmed)
		wantPub    Published
	}{
		{"exact after change confirms", exact(tc.Add(5 * time.Second)), inside, P3, false, Published{Exact, tc.Add(5 * time.Second)}},
		{"exact at change confirms", exact(tc), inside, P3, false, Published{Exact, tc}},
		{"exact one RefreshEvery before change is accepted", exact(tc.Add(-every)), inside, P3, false, Published{Exact, tc.Add(-every)}},
		{"exact older than rounding stays P1 inside window", exact(tc.Add(-every - time.Second)), inside, P1, true, Published{Estimate, tc}},
		{"exact older than rounding is accepted after window", exact(tc.Add(-every - time.Second)), outside, P3, false, Published{Exact, tc.Add(-every - time.Second)}},
		{"not-started inside window", notStarted, inside, P1, true, Published{Starting, tc}},
		{"not-started after window", notStarted, outside, P3, true, Published{Estimate, tc}},
		{"unknown inside window", source.MatchStart{}, inside, P1, true, Published{Estimate, tc}},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			tr := New(testOptions())
			tr.Observe(snap("s1", t0, srv("a", 50)))
			tr.Apply("a", exact(t0.Add(-time.Hour)), t0)
			changed := srv("a", 50)
			changed.Map.Variant = "map-2"
			tr.Observe(snap("s2", tc, changed))

			tr.Apply("a", c.result, c.checkedAt)

			got := mustGet(t, tr, "a")
			if got.Tier != c.wantTier {
				t.Errorf("tier = %v, want %v", got.Tier, c.wantTier)
			}
			if got.ChangeObservedAt.IsZero() == c.wantChange {
				t.Errorf("ChangeObservedAt = %v, want set: %v", got.ChangeObservedAt, c.wantChange)
			}
			wantStart := c.result
			if c.result.State == source.Unknown {
				wantStart = exact(t0.Add(-time.Hour)) // unknown keeps the previous start
			}
			if got.Start != wantStart || !got.CheckedAt.Equal(c.checkedAt) || got.CheckedSnapshotID != "s2" {
				t.Errorf("stored Start %v CheckedAt %v snapshot %q; want %v %v s2", got.Start, got.CheckedAt, got.CheckedSnapshotID, wantStart, c.checkedAt)
			}
			if p, _ := tr.Published("a", c.checkedAt); p != c.wantPub {
				t.Errorf("published = %+v, want %+v", p, c.wantPub)
			}
		})
	}
}

func TestPublished(t *testing.T) {
	tc := t0.Add(every)
	tests := []struct {
		name  string
		setup func(tr *Tracker)
		now   time.Time
		want  Published
	}{
		{"never checked", func(tr *Tracker) {}, tc, Published{}},
		{"unchecked change is an estimate", func(tr *Tracker) {
			s := srv("a", 50)
			s.InstanceID = "inst-2"
			tr.Observe(snap("s2", tc, s))
		}, tc.Add(time.Minute), Published{Estimate, tc}},
		{"starting turns into estimate after window", func(tr *Tracker) {
			s := srv("a", 50)
			s.InstanceID = "inst-2"
			tr.Observe(snap("s2", tc, s))
			tr.Apply("a", notStarted, tc)
		}, tc.Add(21 * time.Minute), Published{Estimate, tc}},
		{"not-started without a change is unknown", func(tr *Tracker) {
			tr.Apply("a", notStarted, t0)
		}, tc, Published{}},
		{"unknown result keeps a confirmed exact start", func(tr *Tracker) {
			tr.Apply("a", exact(t0.Add(-time.Hour)), t0)
			tr.Observe(snap("s2", tc, srv("a", 50)))
			tr.Apply("a", source.MatchStart{}, tc)
		}, tc, Published{Exact, t0.Add(-time.Hour)}},
		{"provider start is exact", func(tr *Tracker) {
			s := srv("a", 50)
			ms := exact(t0.Add(-3 * time.Minute))
			s.MatchStart = &ms
			tr.Observe(snap("s2", tc, s))
		}, tc, Published{Exact, t0.Add(-3 * time.Minute)}},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			tr := New(testOptions())
			tr.Observe(snap("s1", t0, srv("a", 50)))
			c.setup(tr)
			p, ok := tr.Published("a", c.now)
			if !ok || p != c.want {
				t.Errorf("Published = %+v, %v; want %+v, true", p, ok, c.want)
			}
		})
	}
	if _, ok := New(testOptions()).Published("missing", t0); ok {
		t.Error("Published for an untracked server reported ok")
	}
}

// TestQueueOrder checks P0, P1, P2 by ID, then P3 by oldest CheckedAt, and SPEC §7.2 rule 7.
func TestQueueOrder(t *testing.T) {
	tr := New(testOptions())
	all := func(changes map[string]string, extra ...source.Server) []source.Server {
		var out []source.Server
		for _, id := range []string{"a", "b", "d", "e", "f"} {
			s := srv(id, 50)
			if inst, ok := changes[id]; ok {
				s.InstanceID = inst
			}
			out = append(out, s)
		}
		return append(out, extra...)
	}

	tr.Observe(snap("s1", t0, all(nil)...))
	tr.Apply("a", exact(t0.Add(-time.Hour)), t0.Add(5*time.Second))
	tr.Apply("b", exact(t0.Add(-time.Hour)), t0.Add(time.Second))

	tr.Observe(snap("s2", t0.Add(every), all(map[string]string{"d": "d2", "e": "e2"})...))
	tr.Apply("e", notStarted, t0.Add(every+time.Second))
	for _, d := range tr.Queue() {
		if d.ID == "e" {
			t.Fatalf("e was checked in this snapshot but is still queued: %v", tr.Queue())
		}
	}

	tr.Observe(snap("s3", t0.Add(2*every), all(map[string]string{"d": "d3", "e": "e2"}, srv("c", 50))...))
	want := []Due{{"d", P0}, {"e", P1}, {"c", P2}, {"f", P3}, {"b", P3}, {"a", P3}}
	if got := tr.Queue(); !reflect.DeepEqual(got, want) {
		t.Errorf("queue = %v, want %v", got, want)
	}

	tr.Apply("f", exact(t0), t0.Add(2*every))
	if got := queueIDs(tr); !reflect.DeepEqual(got, []string{"d", "e", "c", "b", "a"}) {
		t.Errorf("after checking f: queue = %v", got)
	}
}

func TestEviction(t *testing.T) {
	tr := New(testOptions())
	tr.Observe(snap("s1", t0, srv("x", 50), srv("y", 50)))

	tr.Observe(snap("s2", t0.Add(EvictAfter), srv("y", 50)))
	if got := mustGet(t, tr, "x").Tier; got != None {
		t.Errorf("absent server tier = %v, want none", got)
	}
	if got := queueIDs(tr); !reflect.DeepEqual(got, []string{"y"}) {
		t.Errorf("queue = %v, want [y]", got)
	}

	tr.Observe(snap("s3", t0.Add(EvictAfter+time.Second), srv("y", 50)))
	if _, ok := tr.Get("x"); ok {
		t.Error("server absent for over an hour is still tracked")
	}

	tr.Observe(snap("s4", t0.Add(EvictAfter+every), srv("x", 50), srv("y", 50)))
	if got := mustGet(t, tr, "x").Tier; got != P2 {
		t.Errorf("returning evicted server tier = %v, want P2", got)
	}
}

func TestApplyIgnoresUnqueued(t *testing.T) {
	tr := New(testOptions())
	tr.Observe(snap("s1", t0, srv("a", 0)))
	tr.Apply("a", exact(t0), t0)
	tr.Apply("missing", exact(t0), t0)
	if got := mustGet(t, tr, "a"); got.Start != (source.MatchStart{}) || !got.CheckedAt.IsZero() {
		t.Errorf("empty server was updated by Apply: %+v", got)
	}
	if _, ok := tr.Get("missing"); ok {
		t.Error("Apply created an untracked server")
	}
}
