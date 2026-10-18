package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMediaSourceLinksAreSignedPerFileAndExpire(t *testing.T) {
	h := newTestHub(t)
	agent := newFakeAgent(t, "tok-box")
	agent.dirs["/v"] = true
	agent.files["/v/a.mkv"] = []byte("matroska bytes")
	agent.files["/v/secret.txt"] = []byte("private")
	box := h.machine("box", agent.srv, "linux")
	h.app.media = mediaConfig{url: "http://media", secret: "s", sourceBase: h.srv.URL}

	link := h.app.sourceURL(box, "/v/a.mkv")
	resp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "matroska bytes" {
		t.Fatalf("valid link: %d %q", resp.StatusCode, body)
	}
	// Same signature, different file: refused.
	u, _ := url.Parse(link)
	q := u.Query()
	q.Set("p", "/v/secret.txt")
	u.RawQuery = q.Encode()
	if resp, _ := http.Get(u.String()); resp.StatusCode != 403 {
		t.Fatalf("re-pointed link: %d", resp.StatusCode)
	}
	// Expired: refused.
	exp := time.Now().Add(-time.Minute).Unix()
	old := fmt.Sprintf("%s/media-source?m=%d&p=%s&exp=%d&sig=%s", h.srv.URL, box, url.QueryEscape("/v/a.mkv"), exp, h.app.signSource(box, "/v/a.mkv", exp))
	if resp, _ := http.Get(old); resp.StatusCode != 403 {
		t.Fatalf("expired link: %d", resp.StatusCode)
	}
	// Range passes through to the agent.
	agent.honourRange = true
	req, _ := http.NewRequest(http.MethodGet, link, nil)
	req.Header.Set("Range", "bytes=0-7")
	resp, _ = http.DefaultClient.Do(req)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != 206 || string(body) != "matroska" {
		t.Fatalf("range: %d %q", resp.StatusCode, body)
	}
}

// fakeMedia stands in for the media service.
func fakeMedia(t *testing.T, secret string, plan map[string]any) (*httptest.Server, *[]string) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(401)
			return
		}
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/probe":
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			if !strings.Contains(req["source"], "/media-source?") {
				w.WriteHeader(400)
				return
			}
			json.NewEncoder(w).Encode(plan)
		case "/stream":
			w.Header().Set("Content-Type", "video/mp4")
			io.WriteString(w, "fragmented-mp4-from-"+r.URL.Query().Get("start"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestMediaInfoAndPlayGoThroughTheHubWithAccessChecks(t *testing.T) {
	h := newTestHub(t)
	ann := h.user("ann@example.com", false)
	agent := newFakeAgent(t, "tok-box")
	agent.dirs["/v"] = true
	agent.files["/v/a.mkv"] = []byte("x")
	box := h.machine("box", agent.srv, "linux")
	other := h.machine("other", agent.srv, "linux")
	h.grant(ann, box, "read_only")
	media, seen := fakeMedia(t, "media-secret-123", map[string]any{"mode": "remux", "reason": "Repackaged", "duration": 42.5, "audio": "aac"})
	h.app.media = mediaConfig{url: media.URL, secret: "media-secret-123", sourceBase: h.srv.URL}
	c := h.client()
	c.login("ann@example.com")

	resp, body := c.get(fmt.Sprintf("/api/machines/%d/media-info?path=%s", box, url.QueryEscape("/v/a.mkv")))
	var plan mediaPlan
	json.Unmarshal([]byte(body), &plan)
	if resp.StatusCode != 200 || plan.Mode != "remux" || plan.Duration != 42.5 || !plan.Seekable {
		t.Fatalf("info: %d %s", resp.StatusCode, body)
	}
	resp, body = c.get(fmt.Sprintf("/machines/%d/play?path=%s&mode=remux&audio=aac&start=30", box, url.QueryEscape("/v/a.mkv")))
	if resp.StatusCode != 200 || body != "fragmented-mp4-from-30.00" || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("play: %d %q", resp.StatusCode, body)
	}
	if len(*seen) != 2 || !strings.Contains((*seen)[1], "source=") {
		t.Fatalf("media service calls: %v", *seen)
	}
	// No access to the other machine: refused before the media service is asked.
	if resp, _ := c.get(fmt.Sprintf("/machines/%d/play?path=/v/a.mkv&mode=remux", other)); resp.StatusCode != 403 {
		t.Fatalf("no access: %d", resp.StatusCode)
	}
	if len(*seen) != 2 {
		t.Fatal("the media service must not be called for a refused request")
	}
}

func TestMediaSourceEmulatesRangeForAgentsWithoutIt(t *testing.T) {
	h := newTestHub(t)
	agent := newFakeAgent(t, "tok-box") // honourRange off, like agent 1.1.0
	agent.dirs["/v"] = true
	agent.files["/v/a.mp4"] = []byte("0123456789abcdefghij")
	box := h.machine("box", agent.srv, "linux")
	h.app.media = mediaConfig{url: "http://media", secret: "s", sourceBase: h.srv.URL}
	link := h.app.sourceURL(box, "/v/a.mp4")
	for rng, want := range map[string]string{"bytes=10-14": "abcde", "bytes=15-": "fghij", "bytes=-3": "hij", "bytes=0-0": "0"} {
		req, _ := http.NewRequest(http.MethodGet, link, nil)
		req.Header.Set("Range", rng)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 206 || string(body) != want {
			t.Errorf("%s: %d %q, want %q", rng, resp.StatusCode, body, want)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, link, nil)
	req.Header.Set("Range", "bytes=50-")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 416 {
		t.Errorf("past the end: %d", resp.StatusCode)
	}
}

func TestParseRange(t *testing.T) {
	cases := []struct {
		h        string
		from, to int64
		ok       bool
	}{
		{"bytes=0-9", 0, 9, true}, {"bytes=5-", 5, 99, true}, {"bytes=-10", 90, 99, true}, {"bytes=90-500", 90, 99, true},
		{"bytes=100-", 0, 0, false}, {"bytes=5-2", 0, 0, false}, {"bytes=0-1,5-6", 0, 0, false}, {"items=0-1", 0, 0, false},
	}
	for _, c := range cases {
		f, to, ok := parseRange(c.h, 100)
		if ok != c.ok || (ok && (f != c.from || to != c.to)) {
			t.Errorf("%s: got %d-%d %v", c.h, f, to, ok)
		}
	}
}
