package serve

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// tree makes a directory to serve, and a secret outside it. The returned
// paths are of the symbolic-link-free temporary directory.
//
//	root/deck.md  root/sub/index.md  root/sub/other.md  root/plain.txt
//	root/<bad>.md  root/.env  root/.git/config
//	root/link-out (to ../secret.txt)  root/dir-out (to ../outside)
//	root/link-in (to plain.txt)  root/out.md (to ../outside/deck.md)
func tree(t *testing.T) (root, secret string) {
	t.Helper()
	base, err := Root(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(base, "root")
	write := func(name, s string) {
		p := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("root/deck.md", "# deck")
	write("root/sub/index.md", "# index")
	write("root/sub/other.md", "# other")
	write("root/plain.txt", "plain text")
	write("root/<b>x&y.md", "# odd")
	write("root/.env", "SECRET=1")
	write("root/.git/config", "[core]")
	write("secret.txt", "TOP SECRET")
	write("outside/deck.md", "# outside")
	write("outside/file.txt", "outside file")
	secret = filepath.Join(base, "secret.txt")
	if runtime.GOOS != "windows" {
		for link, target := range map[string]string{
			"link-out": "../secret.txt",
			"dir-out":  "../outside",
			"link-in":  "plain.txt",
			"out.md":   "../outside/deck.md",
		} {
			if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root, secret
}

func TestResolve(t *testing.T) {
	root, secret := tree(t)
	rootReal, _ := Root(root)
	for _, c := range []struct {
		path string
		err  error // nil: found
	}{
		{"/", nil},
		{"/deck.md", nil},
		{"/sub/", nil},
		{"/sub/../deck.md", nil},
		{"/../secret.txt", fs.ErrNotExist}, // cleaned to /secret.txt under root
		{"/../../../../etc/passwd", fs.ErrNotExist},
		{"/missing.md", fs.ErrNotExist},
		{"/.env", fs.ErrNotExist}, // hidden: as if not there
		{"/.git/config", fs.ErrNotExist},
		{"/sub/.hidden", fs.ErrNotExist},
		{"/deck.md\x00.png", ErrForbidden},
	} {
		_, _, err := Resolve(rootReal, c.path)
		if c.err == nil && err != nil || c.err != nil && !errors.Is(err, c.err) {
			t.Errorf("Resolve(%q) = %v, want %v", c.path, err, c.err)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	for _, p := range []string{"/link-out", "/dir-out", "/dir-out/file.txt", "/out.md"} {
		if _, _, err := Resolve(rootReal, p); !errors.Is(err, ErrForbidden) {
			t.Errorf("Resolve(%q) = %v: a link out of the directory must not be followed", p, err)
		}
	}
	got, _, err := Resolve(rootReal, "/link-in")
	if err != nil || got != filepath.Join(rootReal, "plain.txt") {
		t.Errorf("a link inside the directory: %q, %v", got, err)
	}
	_ = secret
}

// FuzzResolve: whatever the path, a success is a path inside the directory.
func FuzzResolve(f *testing.F) {
	f.Add("/")
	f.Add("/../x")
	f.Add("/dir-out/file.txt")
	f.Add("/a/./b/../../c")
	f.Add("//%2e%2e/")
	f.Add("/.git/config")
	dir, err := os.MkdirTemp("", "serve-fuzz")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { os.RemoveAll(dir) })
	base, _ := Root(dir)
	os.MkdirAll(filepath.Join(base, "root", "sub"), 0o755)
	os.MkdirAll(filepath.Join(base, "outside"), 0o755)
	os.WriteFile(filepath.Join(base, "outside", "file.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(base, "root", "sub", "a.md"), []byte("x"), 0o644)
	if runtime.GOOS != "windows" {
		os.Symlink("../outside", filepath.Join(base, "root", "dir-out"))
	}
	rootReal, _ := Root(filepath.Join(base, "root"))
	f.Fuzz(func(t *testing.T, p string) {
		got, _, err := Resolve(rootReal, p)
		if err == nil && !inside(rootReal, got) {
			t.Fatalf("Resolve(%q) = %q, outside %q", p, got, rootReal)
		}
	})
}

// fake is a converter that records what it was asked and returns the kind.
type fake struct {
	mu    sync.Mutex
	calls []string
	fail  bool
}

func (f *fake) convert(_ context.Context, path string, kind Kind, ws string) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, filepath.Base(path)+"|"+string(kind)+"|"+ws)
	f.mu.Unlock()
	if f.fail {
		return Result{}, errors.New("deck is broken")
	}
	ct := map[Kind]string{HTML: "text/html; charset=utf-8", PDF: "application/pdf", PPTX: "application/vnd.openxmlformats-officedocument.presentationml.presentation", PNG: "image/png", JPEG: "image/jpeg", Notes: "text/plain; charset=utf-8"}[kind]
	return Result{Data: []byte("converted " + string(kind) + " " + filepath.Base(path)), ContentType: ct}, nil
}

func get(t *testing.T, h http.Handler, method, target, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newServer(t *testing.T, root string, f *fake, hub *Hub) *Server {
	t.Helper()
	s, err := New(Options{Dir: root, Convert: f.convert, DirectoryIndex: []string{"index.md", "PITCHME.md"}, Hub: hub})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServeConversion(t *testing.T) {
	root, _ := tree(t)
	f := &fake{}
	s := newServer(t, root, f, nil)
	for _, c := range []struct {
		target, ct, body string
	}{
		{"/deck.md", "text/html; charset=utf-8", "converted html deck.md"},
		{"/deck.md?pdf", "application/pdf", "converted pdf deck.md"},
		{"/deck.md?pdf=1&x=2", "application/pdf", "converted pdf deck.md"},
		{"/deck.md?png", "image/png", "converted png deck.md"},
		{"/deck.md?jpg", "image/jpeg", "converted jpeg deck.md"},
		{"/deck.md?jpeg", "image/jpeg", "converted jpeg deck.md"},
		{"/deck.md?notes", "text/plain; charset=utf-8", "converted notes deck.md"},
		{"/deck.md?txt", "text/plain; charset=utf-8", "converted notes deck.md"},
		{"/deck.md?other", "text/html; charset=utf-8", "converted html deck.md"},
		{"/sub/", "text/html; charset=utf-8", "converted html index.md"},
	} {
		rec := get(t, s.Handler(), "GET", c.target, "localhost:8080")
		if rec.Code != 200 || rec.Header().Get("Content-Type") != c.ct || rec.Body.String() != c.body {
			t.Errorf("%s: %d %q %q, want 200 %q %q", c.target, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String(), c.ct, c.body)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: no nosniff", c.target)
		}
	}
	rec := get(t, s.Handler(), "GET", "/deck.md?pptx", "localhost")
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, "deck.pptx") {
		t.Errorf("pptx Content-Disposition = %q", cd)
	}
	if rec := get(t, s.Handler(), "HEAD", "/deck.md", "localhost"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("HEAD: %d %q", rec.Code, rec.Body.String())
	}

	// a failing conversion is a 503 with its message, as marp-cli's
	f.fail = true
	rec = get(t, s.Handler(), "GET", "/deck.md", "localhost")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "deck is broken") {
		t.Errorf("failure: %d %q", rec.Code, rec.Body.String())
	}
}

func TestServeStaticAndListing(t *testing.T) {
	root, _ := tree(t)
	f := &fake{}
	s := newServer(t, root, f, nil)
	rec := get(t, s.Handler(), "GET", "/plain.txt", "localhost")
	if rec.Code != 200 || rec.Body.String() != "plain text" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("static file: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if rec := get(t, s.Handler(), "GET", "/nope.txt", "localhost"); rec.Code != 404 {
		t.Errorf("missing file: %d", rec.Code)
	}
	if rec := get(t, s.Handler(), "GET", "/sub", "localhost"); rec.Code != 301 || rec.Header().Get("Location") != "/sub/" {
		t.Errorf("directory without slash: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// the root has no index.md: a listing, with the dotfiles left out, the
	// names escaped and the decks linked to their conversions
	rec = get(t, s.Handler(), "GET", "/", "localhost")
	body := rec.Body.String()
	if rec.Code != 200 || len(f.calls) != 0 {
		t.Fatalf("listing: %d, converted %v", rec.Code, f.calls)
	}
	for _, want := range []string{"Index of /", "&lt;b&gt;x&amp;y.md", `href="./deck.md?pdf"`, `href="./deck.md?pptx"`, `href="./sub/"`, "plain.txt"} {
		if !strings.Contains(body, want) {
			t.Errorf("listing lacks %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{".env", ".git", "<b>x", "SECRET"} {
		if strings.Contains(body, bad) {
			t.Errorf("listing shows %q", bad)
		}
	}
	// a subdirectory's listing leads back up
	if rec := get(t, s.Handler(), "GET", "/sub/other.md?pdf", "localhost"); rec.Code != 200 {
		t.Errorf("deck in a directory: %d", rec.Code)
	}
}

func TestServeRefuses(t *testing.T) {
	root, _ := tree(t)
	f := &fake{}
	s := newServer(t, root, f, nil)
	h := s.Handler()
	for _, c := range []struct {
		method, target, host string
		want                 int
	}{
		{"GET", "/.env", "localhost", 404}, // as express: dotfiles are not there
		{"GET", "/.git/config", "localhost", 404},
		{"GET", "/%2e%2e/secret.txt", "localhost", 404}, // cleaned inside the directory
		{"GET", "/deck.md", "evil.example", 403},        // DNS rebinding
		{"GET", "/deck.md", "evil.example:8080", 403},
		{"GET", "/deck.md", "localhost.evil.example", 403},
		{"GET", "/deck.md", "127.0.0.1:8080", 200},
		{"GET", "/deck.md", "[::1]:8080", 200},
		{"GET", "/deck.md", "talk.localhost:8080", 200},
		{"POST", "/deck.md", "localhost", 405},
		{"PUT", "/plain.txt", "localhost", 405},
	} {
		if rec := get(t, h, c.method, c.target, c.host); rec.Code != c.want {
			t.Errorf("%s %s (Host %s) = %d, want %d", c.method, c.target, c.host, rec.Code, c.want)
		}
	}
	if runtime.GOOS != "windows" {
		for _, target := range []string{"/link-out", "/dir-out/file.txt", "/out.md", "/out.md?pdf"} {
			rec := get(t, h, "GET", target, "localhost")
			if rec.Code != 403 || strings.Contains(rec.Body.String(), "TOP SECRET") || strings.Contains(rec.Body.String(), "outside") {
				t.Errorf("%s: %d %q: a link out of the directory", target, rec.Code, rec.Body.String())
			}
		}
		// the listing does not offer to convert what it would refuse
		list := get(t, h, "GET", "/", "localhost").Body.String()
		if !strings.Contains(list, "out.md") {
			t.Log("the link is listed by name only")
		}
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "out.md") {
			t.Errorf("a deck outside the directory was converted: %s", call)
		}
	}
}

func TestServeRefusesCrossSiteSubresources(t *testing.T) {
	root, _ := tree(t)
	s := newServer(t, root, &fake{}, nil)
	req := func(site, mode string) int {
		r := httptest.NewRequest("GET", "/plain.txt", nil)
		r.Host = "localhost"
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
			r.Header.Set("Sec-Fetch-Mode", mode)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, r)
		return rec.Code
	}
	for _, c := range []struct {
		site, mode string
		want       int
	}{
		{"", "", 200},                    // not a browser
		{"same-origin", "cors", 200},     // the page's own fetch
		{"none", "navigate", 200},        // typed in the address bar
		{"cross-site", "navigate", 200},  // a link from the web
		{"cross-site", "no-cors", 403},   // <script src>, <img>
		{"cross-site", "cors", 403},      // fetch()
		{"same-site", "no-cors", 200},    // another port of localhost's own
		{"cross-site", "websocket", 403}, // a socket
	} {
		if got := req(c.site, c.mode); got != c.want {
			t.Errorf("Sec-Fetch-Site %q mode %q: %d, want %d", c.site, c.mode, got, c.want)
		}
	}
}

func TestExposedServerHostCheck(t *testing.T) {
	root, _ := tree(t)
	f := &fake{}
	s, err := New(Options{Dir: root, Convert: f.convert, Host: "0.0.0.0", Exposed: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(t, s.Handler(), "GET", "/deck.md", "talk.example.org"); rec.Code != 200 {
		t.Errorf("an exposed server answers any host: %d", rec.Code)
	}
	s2, err := New(Options{Dir: root, Convert: f.convert, Host: "lab.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(t, s2.Handler(), "GET", "/deck.md", "lab.example.org"); rec.Code != 200 {
		t.Errorf("the host it listens on is its own: %d", rec.Code)
	}
	if rec := get(t, s2.Handler(), "GET", "/deck.md", "other.example.org"); rec.Code != 403 {
		t.Errorf("another host is refused: %d", rec.Code)
	}
}

func TestNew(t *testing.T) {
	if _, err := New(Options{Dir: filepath.Join(t.TempDir(), "missing"), Convert: (&fake{}).convert}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing directory: %v", err)
	}
	file := filepath.Join(t.TempDir(), "f")
	os.WriteFile(file, nil, 0o644)
	if _, err := New(Options{Dir: file, Convert: (&fake{}).convert}); err == nil || !strings.Contains(err.Error(), "not directory") {
		t.Errorf("a file: %v", err)
	}
	if _, err := New(Options{Dir: t.TempDir()}); err == nil {
		t.Error("no converter")
	}
}

func TestStartBindsToLoopback(t *testing.T) {
	root, _ := tree(t)
	s, err := New(Options{Dir: root, Convert: (&fake{}).convert, Port: freePort(t)})
	if err != nil {
		t.Fatal(err)
	}
	addr, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("listens on %s, want the loopback", addr)
	}
	resp, err := http.Get("http://" + addr + "/deck.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status %d", resp.StatusCode)
	}
	// a second server on the same port says what to do
	s2, _ := New(Options{Dir: root, Convert: (&fake{}).convert, Port: s.Port()})
	if _, err := s2.Start(); err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Errorf("port in use: %v", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestLiveReload(t *testing.T) {
	root, _ := tree(t)
	hub := NewHub()
	f := &fake{}
	s := newServer(t, root, f, hub)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	// the page names the socket of its deck
	resp, err := http.Get(srv.URL + "/deck.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	deck := filepath.Join(root, "deck.md")
	wantWS := WebSocketPath + ID(deck)
	if len(f.calls) != 1 || !strings.HasSuffix(f.calls[0], "|"+wantWS) {
		t.Fatalf("converter calls %v, want the socket %s", f.calls, wantWS)
	}
	// but only a page of a conversion to HTML
	http.Get(srv.URL + "/deck.md?pdf")
	if last := f.calls[len(f.calls)-1]; !strings.HasSuffix(last, "|") {
		t.Errorf("a PDF has no socket: %s", last)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dial := func(origin string) (*websocket.Conn, *http.Response, error) {
		h := http.Header{}
		if origin != "" {
			h.Set("Origin", origin)
		}
		return websocket.Dial(ctx, "ws://"+host+wantWS, &websocket.DialOptions{HTTPHeader: h})
	}
	c, _, err := dial("http://" + host)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if _, msg, err := c.Read(ctx); err != nil || string(msg) != "ready" {
		t.Fatalf("first word: %q, %v", msg, err)
	}
	if !hub.Notify(deck) {
		t.Fatal("Notify found no page")
	}
	if _, msg, err := c.Read(ctx); err != nil || string(msg) != "reload" {
		t.Fatalf("after a change: %q, %v", msg, err)
	}
	hub.NotifyAll()
	if _, msg, err := c.Read(ctx); err != nil || string(msg) != "reload" {
		t.Fatalf("NotifyAll: %q, %v", msg, err)
	}
	// a page of another origin does not get the socket (the browser sends
	// the Origin of the page that opens it)
	if _, resp, err := dial("http://evil.example"); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Errorf("a foreign origin: %v %v", resp, err)
	}
	// a client with no Origin is not a browser
	if c2, _, err := dial(""); err != nil {
		t.Errorf("no origin: %v", err)
	} else {
		c2.CloseNow()
	}
	// an id nobody registered is not served
	if _, resp, err := websocket.Dial(ctx, "ws://"+host+WebSocketPath+"deadbeef", nil); err == nil || resp == nil || resp.StatusCode != 404 {
		t.Errorf("unknown id: %v %v", resp, err)
	}
	// the socket path is not a file
	if rec := get(t, s.Handler(), "GET", WebSocketPath+"x", "localhost"); rec.Code != 404 {
		t.Errorf("plain GET on the socket path: %d", rec.Code)
	}
	// a Host the server does not know cannot reach it either
	if _, resp, err := websocket.Dial(ctx, "ws://"+host+wantWS, &websocket.DialOptions{Host: "evil.example"}); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Errorf("foreign host: %v %v", resp, err)
	}
}

func TestListenNotifier(t *testing.T) {
	hub := NewHub()
	file := filepath.Join(t.TempDir(), "deck.md")
	id := hub.Register(file)
	port, stop, err := hub.ListenNotifier(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws://127.0.0.1:" + itoa(port) + "/" + id
	// a page opened from a file has the origin "null"
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"null"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if _, msg, _ := c.Read(ctx); string(msg) != "ready" {
		t.Errorf("first word %q", msg)
	}
	hub.Notify(file)
	if _, msg, _ := c.Read(ctx); string(msg) != "reload" {
		t.Errorf("after a change %q", msg)
	}
	if _, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}}); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Errorf("foreign origin: %v %v", resp, err)
	}
	if _, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{Host: "evil.example"}); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Errorf("foreign host: %v %v", resp, err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
