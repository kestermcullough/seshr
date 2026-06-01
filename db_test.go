package main

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	sqldb, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	if _, err := sqldb.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	db := &DB{sqldb: sqldb}
	if err := db.migrate(); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestQueryEscapesCWDPrefixWildcards(t *testing.T) {
	db := newTestDB(t)
	sessions := []Session{
		{Tool: "codex", SessionUUID: "exact", CWD: "/tmp/a_b", LastActive: time.Unix(3, 0)},
		{Tool: "codex", SessionUUID: "child", CWD: "/tmp/a_b/child", LastActive: time.Unix(2, 0)},
		{Tool: "codex", SessionUUID: "wildcard-overmatch", CWD: "/tmp/axb", LastActive: time.Unix(1, 0)},
	}
	if err := db.SyncSessionsScoped(sessions, []string{"codex"}); err != nil {
		t.Fatal(err)
	}

	got, err := db.Query(QueryFilter{CWDPrefix: "/tmp/a_b"})
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := map[string]bool{}
	for _, s := range got {
		gotIDs[s.SessionUUID] = true
	}
	for _, want := range []string{"exact", "child"} {
		if !gotIDs[want] {
			t.Fatalf("missing %q from query result: %#v", want, gotIDs)
		}
	}
	if gotIDs["wildcard-overmatch"] {
		t.Fatalf("LIKE wildcard matched unrelated cwd: %#v", gotIDs)
	}
}

func TestQueryMatchesMultipleCWDPrefixes(t *testing.T) {
	db := newTestDB(t)
	sessions := []Session{
		{Tool: "claude", SessionUUID: "real", CWD: "/real/project", LastActive: time.Unix(2, 0)},
		{Tool: "claude", SessionUUID: "link", CWD: "/link/project", LastActive: time.Unix(1, 0)},
		{Tool: "claude", SessionUUID: "other", CWD: "/other/project", LastActive: time.Unix(3, 0)},
	}
	if err := db.SyncSessionsScoped(sessions, []string{"claude"}); err != nil {
		t.Fatal(err)
	}

	got, err := db.Query(QueryFilter{CWDPrefixes: []string{"/real/project", "/link/project"}})
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := map[string]bool{}
	for _, s := range got {
		gotIDs[s.SessionUUID] = true
	}
	if !gotIDs["real"] || !gotIDs["link"] || gotIDs["other"] {
		t.Fatalf("unexpected query result: %#v", gotIDs)
	}
}

func TestSyncSessionsScopedEmptyScopeDoesNotMarkMissing(t *testing.T) {
	db := newTestDB(t)
	original := Session{
		Tool:        "amp",
		SessionUUID: "existing",
		CWD:         "/repo",
		LastActive:  time.Unix(1, 0),
	}
	if err := db.SyncSessionsScoped([]Session{original}, []string{"amp"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SyncSessionsScoped(nil, []string{}); err != nil {
		t.Fatal(err)
	}
	got, err := db.Query(QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Missing {
		t.Fatalf("empty scoped sync marked existing row missing: %#v", got)
	}
}
