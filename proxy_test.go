package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNormalizeProxyURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "http", input: "http://user:pass@127.0.0.1:8080", want: "http://user:pass@127.0.0.1:8080"},
		{name: "socks5", input: "socks5://127.0.0.1:1080", want: "socks5://127.0.0.1:1080"},
		{name: "sock5 alias", input: "sock5://127.0.0.1:1080", want: "socks5://127.0.0.1:1080"},
		{name: "missing scheme", input: "127.0.0.1:8080", wantErr: true},
		{name: "unsupported scheme", input: "ftp://127.0.0.1:21", wantErr: true},
		{name: "path rejected", input: "http://127.0.0.1:8080/path", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeProxyURL(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeProxyURL(%q) unexpectedly succeeded: %q", test.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeProxyURL(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("normalizeProxyURL(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestProxyConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.conf")
	proxies := []ProxyConfig{
		{ID: "primary", URL: "http://127.0.0.1:8080", Remark: "primary route"},
		{ID: "fallback", URL: "socks5://user:pass@127.0.0.1:1080", Remark: "fallback route"},
	}
	bindings := []ProxyBinding{
		{Provider: "azure", Account: "account-a", ProxyID: "primary", FallbackProxyID: proxyFallbackNone},
		{Provider: "oci", Account: "account-c", ProxyID: "primary", FallbackProxyID: proxyFallbackDirect},
		{Provider: "gcp", Account: "account-b", ProxyID: "primary", FallbackProxyID: "fallback"},
	}
	if err := SaveProxyConfig(path, proxies, bindings); err != nil {
		t.Fatalf("SaveProxyConfig: %v", err)
	}

	cfg := &Config{}
	if err := LoadProxyConfig(path, cfg); err != nil {
		t.Fatalf("LoadProxyConfig: %v", err)
	}
	if !reflect.DeepEqual(cfg.Proxies, proxies) {
		t.Fatalf("proxies round trip mismatch:\n got: %#v\nwant: %#v", cfg.Proxies, proxies)
	}
	if !reflect.DeepEqual(cfg.ProxyBindings, bindings) {
		t.Fatalf("bindings round trip mismatch:\n got: %#v\nwant: %#v", cfg.ProxyBindings, bindings)
	}
}

func TestValidateProxyStateRejectsBrokenFallback(t *testing.T) {
	err := validateProxyState(
		[]ProxyConfig{{ID: "primary", URL: "http://127.0.0.1:8080"}},
		[]ProxyBinding{{Provider: "azure", Account: "account", ProxyID: "primary", FallbackProxyID: "missing"}},
	)
	if err == nil || !strings.Contains(err.Error(), "missing fallback proxy") {
		t.Fatalf("expected missing fallback validation error, got %v", err)
	}
}

func TestValidateProxyStateRejectsReservedProxyID(t *testing.T) {
	err := validateProxyState([]ProxyConfig{{ID: proxyFallbackNone, URL: "http://127.0.0.1:8080"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved proxy id validation error, got %v", err)
	}
}

func TestParseProxyImport(t *testing.T) {
	proxies, importErrors := parseProxyImport("http://127.0.0.1:8080 # first\nsock5://127.0.0.1:1080 # second\n")
	if len(importErrors) != 0 {
		t.Fatalf("parseProxyImport returned errors: %v", importErrors)
	}
	if len(proxies) != 2 {
		t.Fatalf("parseProxyImport returned %d proxies, want 2", len(proxies))
	}
	if proxies[0].Remark != "first" || proxies[1].Remark != "second" {
		t.Fatalf("unexpected remarks: %#v", proxies)
	}
	if proxies[1].URL != "socks5://127.0.0.1:1080" {
		t.Fatalf("sock5 alias was not normalized: %q", proxies[1].URL)
	}
}

func TestProxyPasswordMaskAndRestore(t *testing.T) {
	original := ProxyConfig{ID: "proxy", URL: "http://user:secret@127.0.0.1:8080", Remark: "route"}
	masked := maskProxy(original)
	if strings.Contains(masked.URL, "secret") || !strings.Contains(masked.URL, proxyPasswordMask) {
		t.Fatalf("maskProxy(%q) = %q", original.URL, masked.URL)
	}
	restored := restoreMaskedProxyPassword(masked.URL, original.URL)
	if restored != original.URL {
		t.Fatalf("restoreMaskedProxyPassword() = %q, want %q", restored, original.URL)
	}
	changedUser := strings.Replace(masked.URL, "user:", "other:", 1)
	if got := restoreMaskedProxyPassword(changedUser, original.URL); got != "http://other:secret@127.0.0.1:8080" {
		t.Fatalf("restore with changed username = %q", got)
	}
}

func TestFallbackRoundTripperReplaysRequestBody(t *testing.T) {
	var primaryBody string
	var fallbackBody string
	transport := &fallbackRoundTripper{
		primary: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			data, _ := io.ReadAll(req.Body)
			primaryBody = string(data)
			return nil, errors.New("primary unavailable")
		}),
		fallback: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			data, _ := io.ReadAll(req.Body)
			fallbackBody = string(data)
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodPost, "https://management.azure.com/test", bytes.NewBufferString(`{"action":"start"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if primaryBody != `{"action":"start"}` || fallbackBody != primaryBody {
		t.Fatalf("body replay mismatch: primary=%q fallback=%q", primaryBody, fallbackBody)
	}
}

func TestProxyRouterUsesAccountRoute(t *testing.T) {
	router := NewProxyRouter(
		[]ProxyConfig{{ID: "primary", URL: "http://127.0.0.1:8080"}},
		[]ProxyBinding{{Provider: "azure", Account: "account", ProxyID: "primary", FallbackProxyID: proxyFallbackDirect}},
	)
	routed, err := router.HTTPClient("azure", "account", 60*time.Second)
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}
	if _, ok := routed.Transport.(*fallbackRoundTripper); !ok {
		t.Fatalf("routed client transport = %T, want *fallbackRoundTripper", routed.Transport)
	}
	routedTransport := routed.Transport.(*fallbackRoundTripper)
	directFallback, ok := routedTransport.fallback.(*http.Transport)
	if !ok {
		t.Fatalf("direct fallback transport = %T, want *http.Transport", routedTransport.fallback)
	}
	if directFallback.Proxy != nil {
		t.Fatal("direct fallback must not use environment proxy settings")
	}
	direct, err := router.HTTPClient("azure", "unbound", 60*time.Second)
	if err != nil {
		t.Fatalf("HTTPClient unbound: %v", err)
	}
	if direct.Transport != nil {
		t.Fatalf("unbound account transport = %T, want default direct transport", direct.Transport)
	}
	if routed.Timeout != 60*time.Second {
		t.Fatalf("routed timeout = %v", routed.Timeout)
	}
}

func TestProxyRouterSupportsNoFallback(t *testing.T) {
	router := NewProxyRouter(
		[]ProxyConfig{{ID: "primary", URL: "http://127.0.0.1:8080"}},
		[]ProxyBinding{{Provider: "azure", Account: "account", ProxyID: "primary", FallbackProxyID: proxyFallbackNone}},
	)
	routed, err := router.HTTPClient("azure", "account", 60*time.Second)
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}
	if _, ok := routed.Transport.(*fallbackRoundTripper); ok {
		t.Fatal("none fallback must not install a fallback round tripper")
	}
	primary, ok := routed.Transport.(*http.Transport)
	if !ok || primary.Proxy == nil {
		t.Fatalf("primary proxy transport = %T, want configured *http.Transport", routed.Transport)
	}
}

func TestBuildCloudRuntimeAppliesProxyToWholeAccount(t *testing.T) {
	cfg := &Config{
		AzureAccounts: []AzureConfig{{Name: "azure-account"}},
		GCPAccounts:   []GCPConfig{{Name: "gcp-account"}},
		OCIAccounts:   []OCIConfig{{Name: "oci-account"}},
		Proxies:       []ProxyConfig{{ID: "primary", URL: "http://127.0.0.1:8080"}},
		ProxyBindings: []ProxyBinding{
			{Provider: "azure", Account: "azure-account", ProxyID: "primary", FallbackProxyID: proxyFallbackDirect},
			{Provider: "gcp", Account: "gcp-account", ProxyID: "primary", FallbackProxyID: proxyFallbackDirect},
			{Provider: "oci", Account: "oci-account", ProxyID: "primary", FallbackProxyID: proxyFallbackDirect},
		},
	}
	runtime, err := buildCloudRuntime(cfg)
	if err != nil {
		t.Fatalf("buildCloudRuntime: %v", err)
	}
	services := []CloudService{
		runtime.cloudServices[serviceKey("azure", "azure-account")],
		runtime.cloudServices[serviceKey("gcp", "gcp-account")],
		runtime.cloudServices[serviceKey("oci", "oci-account")],
	}
	for _, service := range services {
		var client *http.Client
		switch typed := service.(type) {
		case *AzureService:
			client = typed.client
		case *GCPService:
			client = typed.client
		case *OCIService:
			client = typed.client
		default:
			t.Fatalf("unexpected service type %T", service)
		}
		if _, ok := client.Transport.(*fallbackRoundTripper); !ok {
			t.Fatalf("service %T transport = %T, want account proxy transport", service, client.Transport)
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
