package voidension

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyHandlerClientIP(t *testing.T) {
	oldConfig, oldPool := configInstance, serverPool
	oldInfo, oldWarn, oldError, oldAccess := infoLog, warnLog, errorLog, accessLog
	t.Cleanup(func() {
		configInstance, serverPool = oldConfig, oldPool
		infoLog, warnLog, errorLog, accessLog = oldInfo, oldWarn, oldError, oldAccess
	})
	logger := log.New(io.Discard, "", 0)
	infoLog, warnLog, errorLog, accessLog = logger, logger, logger, logger
	var received http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	cfg := &configStruct{}
	cfg.App.MaxRetries = 1
	cfg.App.LargeBodyThreshold = 1024
	configInstance = cfg
	serverPool = []*serverStruct{{URL: backend.URL, Alive: true}}
	s := &secure{config: cfg}
	for _, tc := range []struct {
		name, peer, allowed, verified string
		status                        int
	}{
		{"spoofed IPv4", "192.0.2.2:1234", "192.0.2.1", "", http.StatusForbidden},
		{"allowed IPv4", "192.0.2.1:1234", "192.0.2.1", "192.0.2.1", http.StatusOK},
		{"allowed IPv6", "[2001:db8::1]:1234", "2001:0db8:0:0:0:0:0:1", "2001:db8::1", http.StatusOK},
		{"mapped IPv4", "[::ffff:192.0.2.1]:1234", "192.0.2.1", "192.0.2.1", http.StatusOK},
		{"loopback IPv4", "127.0.0.1:1234", "127.0.0.1", "127.0.0.1", http.StatusOK},
		{"loopback IPv6", "[::1]:1234", "::1", "::1", http.StatusOK},
		{"scoped IPv6", "[fe80::1%eth0]:1234", "fe80::1", "fe80::1", http.StatusOK},
		{"malformed peer", "invalid", "192.0.2.1", "", http.StatusForbidden},
		{"invalid peer unrestricted", "invalid:1234", "", "", http.StatusForbidden},
		{"unrestricted", "192.0.2.2:1234", "", "192.0.2.2", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg.Incoming.AllowedIPs = nil
			if tc.allowed != "" {
				cfg.Incoming.AllowedIPs = []string{tc.allowed}
			}
			received = nil
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("test"))
			r.RemoteAddr = tc.peer
			r.Header.Add("X-Real-IP", "192.0.2.1")
			r.Header.Add("X-Real-IP", "127.0.0.1")
			r.Header.Add("X-Forwarded-For", "192.0.2.1, 127.0.0.1")
			r.Header.Add("Forwarded", "for=192.0.2.1")
			w := httptest.NewRecorder()
			s.proxyHandler(w, r)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			if tc.status == http.StatusForbidden {
				if received != nil {
					t.Fatal("denied request reached backend")
				}
				return
			}
			for _, key := range []string{"X-Real-IP", "X-Forwarded-For"} {
				values := received.Values(key)
				if len(values) != 1 || values[0] != tc.verified {
					t.Errorf("%s = %v, want [%s]", key, values, tc.verified)
				}
			}
			if received.Get("Forwarded") != "" {
				t.Error("untrusted Forwarded header reached backend")
			}
		})
	}
}

func TestAllowedIPConfigValidation(t *testing.T) {
	cfg := configStruct{}
	cfg.App.Port = 8080
	cfg.App.DirPath = t.TempDir()
	cfg.App.ReceivePath = "/receive"
	cfg.Outgoing.ServerPostURLs = []string{"http://127.0.0.1:8081"}
	for _, ip := range []string{"127.0.0.1", "::1", "2001:db8::1", "localhost", "", "192.0.2.0/24", "127.0.0.1:80"} {
		cfg.Incoming.AllowedIPs = []string{ip}
		err := validateConfig(cfg)
		valid := ip == "127.0.0.1" || ip == "::1" || ip == "2001:db8::1"
		if (err == nil) != valid {
			t.Errorf("entry %q: error = %v, valid = %v", ip, err, valid)
		}
	}
}
