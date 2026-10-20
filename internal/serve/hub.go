package serve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// WebSocketPath is where the server accepts the live reload sockets of its
// pages, as marp-cli's: /.__marp-cli-watch-notifier__/<id>. The leading dot is
// why no file can be served under it.
const WebSocketPath = "/.__marp-cli-watch-notifier__/"

// Limits on what a page can hold open.
const (
	maxPerID = 16  // sockets for one deck
	maxTotal = 256 // sockets in all
)

// Hub is the live reload: pages register the deck they show, and a change to
// the deck sends "reload" to them. It speaks marp-cli's protocol — a socket
// to <WebSocketPath><id>, "ready" when it is accepted, "reload" on a change —
// with the id the SHA-256 of the deck's absolute path, which a page can only
// know if it was given the page.
type Hub struct {
	mu    sync.Mutex
	ids   map[string]struct{}
	conns map[string]map[*websocket.Conn]struct{}
	total int
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{ids: map[string]struct{}{}, conns: map[string]map[*websocket.Conn]struct{}{}}
}

// ID is the identifier of a deck: the SHA-256 of its absolute path.
func ID(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])
}

// Register announces a deck a page is about to show, and returns its id.
func (h *Hub) Register(path string) string {
	id := ID(path)
	h.mu.Lock()
	h.ids[id] = struct{}{}
	h.mu.Unlock()
	return id
}

// Notify sends "reload" to the pages showing the deck, and reports whether
// there were any.
func (h *Hub) Notify(path string) bool { return h.send(ID(path)) }

// NotifyAll sends "reload" to every page, when something every deck can use
// (a theme, an image, a configuration file) changed.
func (h *Hub) NotifyAll() {
	h.mu.Lock()
	ids := make([]string, 0, len(h.conns))
	for id := range h.conns {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.send(id)
	}
}

func (h *Hub) send(id string) bool {
	h.mu.Lock()
	var cs []*websocket.Conn
	for c := range h.conns[id] {
		cs = append(cs, c)
	}
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, c := range cs {
		// a socket that cannot take a word in 2 s is gone
		if err := c.Write(ctx, websocket.MessageText, []byte("reload")); err != nil {
			c.CloseNow()
		}
	}
	return len(cs) > 0
}

// Handler accepts the sockets: the id is the last element of the path, and
// must have been registered. allow decides an Origin; nil allows the same host
// only (and a missing Origin: a socket not from a browser).
func (h *Hub) Handler(allow func(origin string, r *http.Request) bool) http.Handler {
	if allow == nil {
		allow = sameHostOrigin
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		h.mu.Lock()
		_, known := h.ids[id]
		full := h.total >= maxTotal || len(h.conns[id]) >= maxPerID
		h.mu.Unlock()
		if !known {
			http.Error(w, "unknown deck", http.StatusNotFound)
			return
		}
		if full {
			http.Error(w, "too many sockets", http.StatusServiceUnavailable)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !allow(o, r) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		// The origin was checked above, which the library's own check, that
		// compares the Origin with the Host only, cannot do for the "null" a
		// page opened from a file sends.
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		c.SetReadLimit(512)
		h.mu.Lock()
		if h.conns[id] == nil {
			h.conns[id] = map[*websocket.Conn]struct{}{}
		}
		h.conns[id][c] = struct{}{}
		h.total++
		h.mu.Unlock()
		defer func() {
			h.mu.Lock()
			delete(h.conns[id], c)
			if len(h.conns[id]) == 0 {
				delete(h.conns, id)
			}
			h.total--
			h.mu.Unlock()
			c.CloseNow()
		}()
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		err = c.Write(ctx, websocket.MessageText, []byte("ready"))
		cancel()
		if err != nil {
			return
		}
		// Reads nothing, but notices the close.
		<-c.CloseRead(context.Background()).Done()
	})
}

// sameHostOrigin allows an Origin whose host is the request's Host.
func sameHostOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

// fileOrigin allows what a page of the watch mode shows: the "null" origin of
// a file opened in the browser, and a page served from the loopback.
func fileOrigin(origin string, r *http.Request) bool {
	if origin == "null" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

// ListenNotifier starts the hub's socket server for the watch mode, where the
// pages are files and not served by anyone: on the loopback, from port (marp's
// 37717 when it is 0) up to the first one that is free. It returns the port
// and a stop function.
func (h *Hub) ListenNotifier(port int) (int, func(), error) {
	if port == 0 {
		port = 37717
	}
	var ln net.Listener
	var err error
	for p := port; p < port+100 && p < 65536; p++ {
		ln, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err == nil {
			port = p
			break
		}
	}
	if err != nil {
		return 0, nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", h.Handler(fileOrigin))
	srv := &http.Server{Handler: hostGuard(mux, nil), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	return port, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		srv.Close()
	}, nil
}

// isLoopbackHost reports whether a host name is the loopback's: localhost, a
// name under it (browsers resolve those to the loopback), or a loopback
// address.
func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// hostGuard refuses the requests whose Host is not one of allowed, or (nil)
// not the loopback's: a page that a DNS rebinding made believe the server was
// its own origin sends its own name as the Host.
func hostGuard(next http.Handler, allowed func(host string) bool) http.Handler {
	if allowed == nil {
		allowed = isLoopbackHost
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !allowed(host) {
			http.Error(w, "unknown host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
