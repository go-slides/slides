package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// syncBuf is a buffer a command writes to while a test reads it.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitFor waits until the buffer holds s.
func (s *syncBuf) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(s.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("never saw %q in:\n%s", want, s.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// command is a running slides command: stop ends it by its context and says
// what it returned.
type command struct {
	stderr *syncBuf
	stop   func() error
}

func start(t *testing.T, args ...string) *command {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c := RootCmd()
	stderr := &syncBuf{}
	c.SetArgs(args)
	c.SetOut(io.Discard)
	c.SetErr(stderr)
	done := make(chan error, 1)
	go func() { done <- c.ExecuteContext(ctx) }()
	cmd := &command{stderr: stderr}
	stopped := false
	cmd.stop = func() error {
		stopped = true
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Error("the command did not stop in 10 s")
			return nil
		}
	}
	t.Cleanup(func() {
		if !stopped {
			cmd.stop()
		}
	})
	return cmd
}

func freeTCPPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func httpGet(t *testing.T, url, host string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func TestServerEndToEnd(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "deck.md", "# Served\n\n<!-- a note -->\n\n---\n\nsecond\n")
	write(t, dir, ".env", "SECRET=1")
	port := freeTCPPort(t)
	t.Setenv("PORT", port)
	c := start(t, "--server", dir)
	c.stderr.waitFor(t, "listened at http://127.0.0.1:"+port+"/")
	base := "http://127.0.0.1:" + port

	code, body, hdr := httpGet(t, base+"/deck.md", "")
	if code != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/html") || !strings.Contains(body, `<h1 id="served">Served</h1>`) {
		t.Fatalf("deck.md: %d %q\n%.300s", code, hdr.Get("Content-Type"), body)
	}
	m := regexp.MustCompile(`__slidesWatchWS="(/\.__marp-cli-watch-notifier__/[0-9a-f]{64})"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the page does not name its socket:\n%s", body[max(0, len(body)-600):])
	}
	if code, body, _ := httpGet(t, base+"/deck.md?notes", ""); code != 200 || body != "a note\n\n---\n\n" {
		t.Errorf("?notes: %d %q", code, body)
	}
	if code, body, hdr := httpGet(t, base+"/deck.md?pdf", ""); code != 200 || hdr.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(body, "%PDF") {
		t.Errorf("?pdf: %d %q %.20q", code, hdr.Get("Content-Type"), body)
	}
	if code, body, hdr := httpGet(t, base+"/deck.md?png", ""); code != 200 || hdr.Get("Content-Type") != "image/png" || !strings.HasPrefix(body, "\x89PNG") {
		t.Errorf("?png: %d %q", code, hdr.Get("Content-Type"))
	}
	if code, body, _ := httpGet(t, base+"/", ""); code != 200 || !strings.Contains(body, "deck.md") || strings.Contains(body, ".env") {
		t.Errorf("index: %d\n%s", code, body)
	}
	if code, _, _ := httpGet(t, base+"/.env", ""); code != 404 {
		t.Errorf(".env: %d", code)
	}
	if code, _, _ := httpGet(t, base+"/deck.md", "evil.example"); code != 403 {
		t.Errorf("a foreign Host: %d", code)
	}

	// the page's socket gets "ready", then "reload" when the deck changes
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+port+m[1], &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {base}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if _, msg, err := ws.Read(ctx); err != nil || string(msg) != "ready" {
		t.Fatalf("first word %q, %v", msg, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deck.md"), []byte("# Changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, msg, err := ws.Read(ctx); err != nil || string(msg) != "reload" {
		t.Fatalf("after a change %q, %v", msg, err)
	}
	if _, body, _ := httpGet(t, base+"/deck.md", ""); !strings.Contains(body, "Changed") {
		t.Error("the changed deck is converted again")
	}
	if err := c.stop(); err != nil {
		t.Errorf("stopping the server: %v", err)
	}
	if _, err := http.Get(base + "/deck.md"); err == nil {
		t.Error("the server should be down")
	}
}

func TestServerPreviewOpensTheBrowser(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "deck.md", "# D")
	port := freeTCPPort(t)
	t.Setenv("PORT", port)
	var mu sync.Mutex
	var opened []string
	old := browserOpen
	browserOpen = func(u string) error { mu.Lock(); opened = append(opened, u); mu.Unlock(); return nil }
	defer func() { browserOpen = old }()
	c := start(t, "-s", "-p", dir)
	c.stderr.waitFor(t, "listened at")
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(opened)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 1 || opened[0] != "http://127.0.0.1:"+port+"/" {
		t.Errorf("opened %v", opened)
	}
}

func TestServerArguments(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--server"}, "needs a directory"},
		{[]string{"--server", dir, dir}, "just one directory"},
		{[]string{"--server", "-I", dir, filepath.Join(dir, "a.md")}, "cannot pass files together with input directory"},
		{[]string{"--server", filepath.Join(dir, "missing")}, "is not found"},
		{[]string{"--server", filepath.Join(dir, "nofile.md")}, "is not found"},
	} {
		if _, _, err := run(t, c.args...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.args, err, c.want)
		}
	}
	// a port that is taken
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Setenv("PORT", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))
	if _, _, err := run(t, "--server", dir); err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Errorf("port in use: %v", err)
	}
}

func TestWatchRebuildsAndNotifies(t *testing.T) {
	dir := t.TempDir()
	deck := write(t, dir, "deck.md", "# First\n")
	themeFile := write(t, dir, "mine.css", "/* @theme mine */\nsection { background: #111111; }")
	write(t, dir, "other.md", "# Other\n")
	c := start(t, "--watch", "--theme-set", themeFile, deck)
	c.stderr.waitFor(t, "[Watch mode] Start watching")
	out := filepath.Join(dir, "deck.html")
	page, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`__slidesWatchWS="(ws://127\.0\.0\.1:\d+/[0-9a-f]{64})"`).FindSubmatch(page)
	if m == nil || !strings.Contains(string(page), "First") {
		t.Fatalf("the page does not open the watcher's socket:\n%.200s", page)
	}
	if exists(filepath.Join(dir, "other.html")) {
		t.Error("only the deck asked for is built")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// a page opened from a file has the origin "null"
	ws, _, err := websocket.Dial(ctx, string(m[1]), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"null"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if _, msg, err := ws.Read(ctx); err != nil || string(msg) != "ready" {
		t.Fatalf("first word %q, %v", msg, err)
	}
	// the deck changes: built again, then the page told
	os.WriteFile(deck, []byte("# Second\n"), 0o644)
	if _, msg, err := ws.Read(ctx); err != nil || string(msg) != "reload" {
		t.Fatalf("after the deck changed: %q, %v", msg, err)
	}
	if page, _ := os.ReadFile(out); !strings.Contains(string(page), "Second") {
		t.Error("the page was told before it was built")
	}
	// the theme changes: every deck is built again
	os.WriteFile(themeFile, []byte("/* @theme mine */\nsection { background: #222222; }"), 0o644)
	if _, msg, err := ws.Read(ctx); err != nil || string(msg) != "reload" {
		t.Fatalf("after the theme changed: %q, %v", msg, err)
	}
	// a deck that does not build is reported and the watch goes on
	os.WriteFile(deck, []byte("<!-- _kind: nosuchkind -->\n\n# Broken\n"), 0o644)
	c.stderr.waitFor(t, "failed converting")
	os.WriteFile(deck, []byte("# Third\n"), 0o644)
	if _, msg, err := ws.Read(ctx); err != nil || string(msg) != "reload" {
		t.Fatalf("after the repair: %q, %v", msg, err)
	}
	if page, _ := os.ReadFile(out); !strings.Contains(string(page), "Third") {
		t.Error("Third not built")
	}
	if err := c.stop(); err != nil {
		t.Errorf("stopping the watch: %v", err)
	}
}

func TestWatchInputDirSeesNewDecks(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	os.MkdirAll(in, 0o755)
	write(t, in, "a.md", "# A\n")
	out := filepath.Join(dir, "out")
	c := start(t, "-w", "-I", in, "-o", out)
	c.stderr.waitFor(t, "[Watch mode] Start watching")
	if !exists(filepath.Join(out, "a.html")) {
		t.Fatal("a.html")
	}
	os.MkdirAll(filepath.Join(in, "sub"), 0o755)
	time.Sleep(200 * time.Millisecond)
	write(t, in, "sub/b.md", "# B\n")
	deadline := time.Now().Add(15 * time.Second)
	for !exists(filepath.Join(out, "sub", "b.html")) {
		if time.Now().After(deadline) {
			t.Fatalf("a deck made in a new directory is not built:\n%s", c.stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestWatchRefusals(t *testing.T) {
	dir := t.TempDir()
	deck := write(t, dir, "d.md", "# D")
	if _, _, err := run(t, "-w", "-o", "-", deck); err == nil || !strings.Contains(err.Error(), "standard output") {
		t.Errorf("-w -o -: %v", err)
	}
	if _, _, err := run(t, "-w", "-"); err == nil || !strings.Contains(err.Error(), "standard input") {
		t.Errorf("-w -: %v", err)
	}
}

func TestPreviewOpensTheHTML(t *testing.T) {
	dir := t.TempDir()
	deck := write(t, dir, "d.md", "# D")
	var mu sync.Mutex
	var opened []string
	old := browserOpen
	browserOpen = func(u string) error { mu.Lock(); opened = append(opened, u); mu.Unlock(); return nil }
	defer func() { browserOpen = old }()
	c := start(t, "--preview", deck)
	c.stderr.waitFor(t, "[Watch mode] Start watching")
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 1 || !strings.HasPrefix(opened[0], "file://") || !strings.HasSuffix(opened[0], "/d.html") {
		t.Errorf("opened %v", opened)
	}
}

func TestOpenBrowserOnlyOpensWebAddresses(t *testing.T) {
	for _, u := range []string{"javascript:alert(1)", "ftp://x/", "/etc/passwd", "-a Calculator", ""} {
		if err := openBrowser(u); err == nil {
			t.Errorf("openBrowser(%q) should refuse", u)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	for h, want := range map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true, "127.0.0.2": true, "0.0.0.0": false, "::": false, "192.168.1.2": false, "example.org": false, "": false} {
		if isLoopback(h) != want {
			t.Errorf("isLoopback(%q) = %v", h, !want)
		}
	}
}
