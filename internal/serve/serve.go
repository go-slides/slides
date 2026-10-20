package serve

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Kind is what a deck is converted to.
type Kind string

// The conversions a request can ask for, with marp-cli's query keys:
// deck.md?pdf, ?pptx, ?png, ?jpg (or ?jpeg), ?txt (or ?notes).
const (
	HTML  Kind = "html"
	PDF   Kind = "pdf"
	PPTX  Kind = "pptx"
	PNG   Kind = "png"
	JPEG  Kind = "jpeg"
	Notes Kind = "notes"
)

// Result is a converted deck.
type Result struct {
	Data        []byte
	ContentType string
}

// Converter converts the Markdown file at path (absolute, inside the served
// directory) to kind. ws is the path of the live reload socket the HTML page
// should open, or "" for none.
type Converter func(ctx context.Context, path string, kind Kind, ws string) (Result, error)

// Options configures a Server.
type Options struct {
	// Dir is the directory to serve.
	Dir string
	// Host and Port are what to listen on. The host defaults to 127.0.0.1;
	// the port to the PORT environment variable, else 8080, as marp-cli's.
	Host string
	Port int
	// Convert converts a deck.
	Convert Converter
	// DirectoryIndex lists the decks a directory shows as its index page, in
	// order (marp-cli: index.md, PITCHME.md); a directory with none shows a
	// listing.
	DirectoryIndex []string
	// Hub, when not nil, is the live reload: converted pages open its socket.
	Hub *Hub
	// Log receives the errors of conversions; nil drops them.
	Log func(format string, args ...any)
	// Exposed says the listener is not the loopback and the Host header is
	// not checked; the caller has warned.
	Exposed bool
}

// Server serves a directory.
type Server struct {
	root string
	opts Options
	mux  http.Handler
	http *http.Server
	ln   net.Listener
}

// DefaultPort is marp-cli's.
const DefaultPort = 8080

// New prepares a server for opts.Dir.
func New(o Options) (*Server, error) {
	if o.Dir == "" {
		return nil, errors.New("serve: no directory")
	}
	if o.Convert == nil {
		return nil, errors.New("serve: no converter")
	}
	root, err := Root(o.Dir)
	if err != nil {
		return nil, err
	}
	if o.Host == "" {
		o.Host = "127.0.0.1"
	}
	if o.Port == 0 {
		o.Port = DefaultPort
		if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 && p < 65536 {
			o.Port = p
		}
	}
	s := &Server{root: root, opts: o}
	mux := http.NewServeMux()
	if o.Hub != nil {
		mux.Handle(WebSocketPath, o.Hub.Handler(nil))
	}
	mux.HandleFunc("/", s.serve)
	allowed := isLoopbackHost
	switch {
	case o.Exposed:
		allowed = func(string) bool { return true }
	case !isLoopbackHost(o.Host):
		h := o.Host
		allowed = func(host string) bool { return isLoopbackHost(host) || strings.EqualFold(host, h) }
	}
	s.mux = hostGuard(secure(mux), allowed)
	return s, nil
}

// Handler is the server's handler, for tests and for embedding.
func (s *Server) Handler() http.Handler { return s.mux }

// Root is the directory served, resolved.
func (s *Server) Root() string { return s.root }

// Start listens and serves in the background, and returns the address. A port
// that is taken is an error naming the PORT variable, as marp-cli's.
func (s *Server) Start() (string, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(s.opts.Host, strconv.Itoa(s.opts.Port)))
	if err != nil {
		if isAddrInUse(err) {
			return "", fmt.Errorf("listen port %d is already used in the other process; try again after closing the relevant process, or specify another port number through PORT env", s.opts.Port)
		}
		return "", err
	}
	s.ln = ln
	s.http = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go s.http.Serve(ln)
	return ln.Addr().String(), nil
}

// isAddrInUse reports whether a listen error is a port already taken (the
// errno differs on Windows, where only the text is common).
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	m := err.Error()
	return strings.Contains(m, "address already in use") || strings.Contains(m, "Only one usage of each socket address")
}

// Port is the port Start listened on.
func (s *Server) Port() int {
	if s.ln == nil {
		return s.opts.Port
	}
	return s.ln.Addr().(*net.TCPAddr).Port
}

// Stop shuts the server down.
func (s *Server) Stop() {
	if s.http == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.http.Shutdown(ctx)
	s.http.Close()
}

// secure adds the headers a local server should send, and allows GET and HEAD.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// A page of another site can load our files as scripts, images or
		// styles and learn from the effect what a deck holds. Browsers say
		// what a request is for (Sec-Fetch-*): only a navigation from another
		// site is allowed — a link to a deck from a page of the web is fine,
		// an <script src> or a fetch to it is not.
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" && r.Header.Get("Sec-Fetch-Mode") != "navigate" {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// kindOf is marp-cli's choice of conversion from the query: the first of pdf,
// pptx, png, jpg or jpeg, txt or notes that is a key; HTML otherwise.
func kindOf(q url.Values) Kind {
	has := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := q[k]; ok {
				return true
			}
		}
		return false
	}
	switch {
	case has("pdf"):
		return PDF
	case has("pptx"):
		return PPTX
	case has("png"):
		return PNG
	case has("jpg", "jpeg"):
		return JPEG
	case has("txt", "notes"):
		return Notes
	}
	return HTML
}

func (s *Server) logf(format string, args ...any) {
	if s.opts.Log != nil {
		s.opts.Log(format, args...)
	}
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	p, st, err := Resolve(s.root, r.URL.Path)
	switch {
	case errors.Is(err, ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	case errors.Is(err, fs.ErrNotExist):
		http.NotFound(w, r)
		return
	case err != nil:
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if st.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			u := *r.URL
			u.Path = path.Clean("/"+r.URL.Path) + "/"
			http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
			return
		}
		for _, name := range s.opts.DirectoryIndex {
			ip, ist, err := Resolve(s.root, path.Join(r.URL.Path, name))
			if err == nil && ist.Mode().IsRegular() && IsMarkdown(ip) {
				s.convert(w, r, ip)
				return
			}
		}
		s.list(w, r, p)
		return
	}
	if IsMarkdown(p) && st.Mode().IsRegular() {
		s.convert(w, r, p)
		return
	}
	if !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if ct := mime.TypeByExtension(filepath.Ext(p)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, filepath.Base(p), st.ModTime(), f)
}

// convert answers with the deck at p converted as the query asks.
func (s *Server) convert(w http.ResponseWriter, r *http.Request, p string) {
	kind := kindOf(r.URL.Query())
	ws := ""
	if s.opts.Hub != nil && kind == HTML {
		ws = WebSocketPath + s.opts.Hub.Register(p)
	}
	res, err := s.opts.Convert(r.Context(), p, kind, ws)
	if err != nil {
		// marp-cli answers 503 with the error's text
		s.logf("%s: %v", p, err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	h := w.Header()
	h.Set("Content-Type", res.ContentType)
	h.Set("Cache-Control", "no-cache")
	if kind == PPTX {
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": base + ".pptx"}))
	}
	h.Set("Content-Length", strconv.Itoa(len(res.Data)))
	if r.Method != http.MethodHead {
		w.Write(res.Data)
	}
}

type entry struct {
	Name        string
	Href        string
	Dir         bool
	Convertible bool
	NodeModules bool
	PDF, PPTX   string
}

var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Index of {{.Path}}</title>
<style>
body{background:#fff;color:#444;font-family:Helvetica,Arial,sans-serif;font-size:15px}
h1{font-size:26px}
ul#index{list-style:none;margin:1em 2em;max-width:720px;padding:0}
ul#index li{display:flex;gap:.8em;height:30px;margin:5px 0 0;opacity:.6}
ul#index li.convertible{opacity:1}
ul#index li.directory a.file{font-weight:bold}
ul#index li a{color:inherit}
ul#index li.node-modules{opacity:.3}
ul#index li:not(.show-all).node-modules{display:none}
ul#index li a.link:not(.file){font-size:.8em;padding-top:.2em}
</style></head><body>
<h1>Index of {{.Path}}</h1>
<ul id="index">
{{range .Entries}}<li class="{{if .Convertible}}convertible {{end}}{{if .Dir}}directory {{end}}{{if .NodeModules}}node-modules{{end}}"><a class="link file" href="{{.Href}}">{{.Name}}</a>{{if .Convertible}}<a class="link pdf" href="{{.PDF}}">PDF</a><a class="link pptx" href="{{.PPTX}}">PPTX</a>{{end}}</li>
{{end}}</ul>
</body></html>
`))

// list writes the index of a directory: its entries, the dotfiles left out,
// the decks with links to their PDF and PPTX conversions. Names are
// HTML-escaped by the template and URL-escaped in the links.
func (s *Server) list(w http.ResponseWriter, r *http.Request, dir string) {
	des, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	var entries []entry
	if dir != s.root {
		entries = append(entries, entry{Name: "..", Href: "../", Dir: true})
	}
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		// What a link points at is judged as a request would be: a link
		// that leaves the directory is shown as nothing special, and is
		// refused when followed.
		isDir := de.IsDir()
		if de.Type()&fs.ModeSymlink != 0 {
			if _, st, err := Resolve(s.root, path.Join(r.URL.Path, name)); err == nil {
				isDir = st.IsDir()
			}
		}
		e := entry{Name: name, Dir: isDir, NodeModules: isDir && name == "node_modules"}
		// "./" keeps a name with a colon from being read as a scheme
		e.Href = "./" + (&url.URL{Path: name}).EscapedPath()
		if isDir {
			e.Href += "/"
		} else if IsMarkdown(name) {
			e.Convertible = true
			e.PDF = e.Href + "?pdf"
			e.PPTX = e.Href + "?pptx"
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if (a.Name == "..") != (b.Name == "..") {
			return a.Name == ".."
		}
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	indexTmpl.Execute(w, struct {
		Path    string
		Entries []entry
	}{path.Clean("/" + r.URL.Path), entries})
}
