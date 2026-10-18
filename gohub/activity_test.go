package main

import (
	"strconv"
	"strings"
	"testing"
)

func TestDescribeRequestNamesTheFileAndWhereItIs(t *testing.T) {
	cases := []struct{ raw, action, file, folder string }{
		{`/api/download?path=C%3A%5CTemp%5Creport.pdf`, "Downloaded", "report.pdf", `C:\Temp\`},
		{`/api/preview?path=%2Fhome%2Fme%2Fcat.jpg`, "Previewed", "cat.jpg", "/home/me/"},
		{`/api/list?path=%2Fhome%2Fme%2F`, "Listed folder", "me", "/home/"},
		{`/api/list?path=C%3A%5C`, "Listed folder", `C:\`, ""},
		{`/api/upload?path=D%3A%5CInbox`, "Uploaded to", "Inbox", `D:\`},
		{`/api/search?path=%2F&query=invoice`, "Searched for “invoice” in", "/", ""},
		{`/api/mkdir`, "Created folder", "", ""},
		{`/api/transfers`, "Checked transfers", "", ""},
	}
	for _, c := range cases {
		_, action, file, folder := describeRequest(c.raw)
		if action != c.action || file != c.file || folder != c.folder {
			t.Errorf("%s: got (%q, %q, %q), want (%q, %q, %q)", c.raw, action, file, folder, c.action, c.file, c.folder)
		}
	}
}

func activitySetup(t *testing.T) (*testHub, *client, *fakeAgent, *fakeAgent) {
	h := newTestHub(t)
	ann := h.user("ann@example.com", false)
	one, two := newFakeAgent(t, "tok-Alpha"), newFakeAgent(t, "tok-Bravo")
	a, b := h.machine("Alpha", one.srv, "linux"), h.machine("Bravo", two.srv, "windows")
	h.grant(ann, a, "read_only")
	h.grant(ann, b, "read_only")
	one.activity = []ActivityEntry{
		{Time: "2026-10-18T10:00:05Z", Method: "GET", Path: "/api/transfers", Status: 200, Client: "10.0.0.9"},
		{Time: "2026-10-18T10:00:04Z", Method: "GET", Path: "/api/download?path=%2Fdata%2Fnotes.txt", Status: 200, Client: "10.0.0.9"},
		{Time: "2026-10-18T10:00:03Z", Method: "POST", Path: "/api/delete", Status: 500, Client: "10.0.0.9"},
	}
	two.activity = []ActivityEntry{
		{Time: "2026-10-18T10:00:02Z", Method: "GET", Path: "/api/list?path=C%3A%5CUsers", Status: 403, Client: "10.0.0.9"},
		{Time: "2026-10-18T10:00:01Z", Method: "GET", Path: "/api/netstats", Status: 200, Client: "10.0.0.9"},
	}
	c := h.client()
	c.login("ann@example.com")
	return h, c, one, two
}

func TestActivityStartsHiddenAndAsksNoAgents(t *testing.T) {
	_, c, one, two := activitySetup(t)
	_, body := c.get("/activity")
	if !strings.Contains(body, "press <span") || strings.Contains(body, "notes.txt") {
		t.Fatal("the list should be hidden until Show activity is pressed")
	}
	if one.activityCalls+two.activityCalls != 0 {
		t.Fatal("opening the page must not ask any agent for its log")
	}
}

func TestActivityWithNoFiltersShowsEverythingExceptBackgroundChecks(t *testing.T) {
	_, c, _, _ := activitySetup(t)
	_, body := c.get("/activity?show=1")
	for _, want := range []string{"notes.txt", "from /data/", "Deleted", "Users", "Listed folder", "3 matching entries"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "Checked transfers") || strings.Contains(body, "Read network stats") {
		t.Error("background checks should be hidden by default")
	}
	_, body = c.get("/activity?show=1&background=1")
	if !strings.Contains(body, "Checked transfers") || !strings.Contains(body, "5 matching entries") {
		t.Error("ticking the box should include background checks")
	}
}

func TestActivityFiltersByMachineMethodAndStatus(t *testing.T) {
	h, c, one, two := activitySetup(t)
	var bravo int64
	h.store.db.QueryRow(`SELECT id FROM machines WHERE name = 'Bravo'`).Scan(&bravo)
	calls := one.activityCalls
	_, body := c.get("/activity?show=1&machine=" + strconv.FormatInt(bravo, 10))
	if strings.Contains(body, "notes.txt") || !strings.Contains(body, "Users") || one.activityCalls != calls {
		t.Error("machine filter should only ask that machine")
	}
	_, body = c.get("/activity?show=1&method=POST")
	if !strings.Contains(body, "Deleted") || strings.Contains(body, "notes.txt") {
		t.Error("method filter")
	}
	_, body = c.get("/activity?show=1&status=4xx")
	if !strings.Contains(body, "Users") || strings.Contains(body, "notes.txt") || strings.Contains(body, "Deleted") {
		t.Error("status filter")
	}
	_ = two
}

func TestReadablePathDecodesTheQuery(t *testing.T) {
	cases := map[string]string{
		`/api/download?path=C%3A%5CTemp%5Clanhub-baseline-20261015223608%5Cdl-25m.bin`: `/api/download?path=C:\Temp\lanhub-baseline-20261015223608\dl-25m.bin`,
		`/api/search?path=%2Fhome%2Fme&query=my+invoice`:                               `/api/search?path=/home/me&query=my invoice`,
		`/api/mkdir`:            `/api/mkdir`,
		`/api/list?path=%ZZbad`: `/api/list?path=%ZZbad`,
	}
	for in, want := range cases {
		if got := readablePath(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}
