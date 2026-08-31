package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

type ProxyRouter struct {
	proxies  map[string]ProxyConfig
	bindings map[string]ProxyBinding
}

func NewProxyRouter(proxies []ProxyConfig, bindings []ProxyBinding) *ProxyRouter {
	router := &ProxyRouter{
		proxies:  make(map[string]ProxyConfig, len(proxies)),
		bindings: make(map[string]ProxyBinding, len(bindings)),
	}
	for _, proxy := range proxies {
		router.proxies[proxy.ID] = proxy
	}
	for _, binding := range bindings {
		router.bindings[proxyBindingKey(binding.Provider, binding.Account)] = binding
	}
	return router
}

func (r *ProxyRouter) HasBinding(provider, account string) bool {
	if r == nil {
		return false
	}
	_, ok := r.bindings[proxyBindingKey(provider, account)]
	return ok
}

func (r *ProxyRouter) HTTPClient(provider, account string, timeout time.Duration) (*http.Client, error) {
	if r == nil {
		return &http.Client{Timeout: timeout}, nil
	}
	binding, ok := r.bindings[proxyBindingKey(provider, account)]
	if !ok {
		return &http.Client{Timeout: timeout}, nil
	}
	primary, ok := r.proxies[binding.ProxyID]
	if !ok {
		return nil, fmt.Errorf("primary proxy %q not found", binding.ProxyID)
	}
	primaryTransport, err := transportForProxy(primary.URL)
	if err != nil {
		return nil, err
	}

	var fallback http.RoundTripper = directTransport()
	if binding.FallbackProxyID != "" {
		fallbackProxy, ok := r.proxies[binding.FallbackProxyID]
		if !ok {
			return nil, fmt.Errorf("fallback proxy %q not found", binding.FallbackProxyID)
		}
		fallback, err = transportForProxy(fallbackProxy.URL)
		if err != nil {
			return nil, err
		}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &fallbackRoundTripper{
			primary:  primaryTransport,
			fallback: fallback,
		},
	}, nil
}

func normalizeProxyURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("proxy URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid proxy URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "sock5" {
		scheme = "socks5"
		parsed.Scheme = scheme
	}
	if scheme != "http" && scheme != "https" && scheme != "socks5" && scheme != "socks5h" {
		return "", fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("proxy host is required")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("proxy URL cannot contain a path")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func transportForProxy(raw string) (http.RoundTripper, error) {
	normalized, err := normalizeProxyURL(raw)
	if err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(normalized)
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		transport := directTransport()
		transport.Proxy = http.ProxyURL(parsed)
		return transport, nil
	}

	var auth *xproxy.Auth
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		auth = &xproxy.Auth{User: parsed.User.Username(), Password: password}
	}
	dialer, err := xproxy.SOCKS5("tcp", parsed.Host, auth, xproxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("create SOCKS5 dialer: %w", err)
	}
	transport := directTransport()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if contextDialer, ok := dialer.(xproxy.ContextDialer); ok {
			return contextDialer.DialContext(ctx, network, address)
		}
		return dialer.Dial(network, address)
	}
	return transport, nil
}

func directTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return transport
}

type fallbackRoundTripper struct {
	primary  http.RoundTripper
	fallback http.RoundTripper
}

func (t *fallbackRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.primary.RoundTrip(req)
	if err == nil && !isProxyGatewayFailure(resp.StatusCode) {
		return resp, nil
	}
	if resp != nil {
		if resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}
	fallbackRequest, cloneErr := cloneRequestForRetry(req)
	if cloneErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, cloneErr
	}
	return t.fallback.RoundTrip(fallbackRequest)
}

func isProxyGatewayFailure(status int) bool {
	return status == http.StatusProxyAuthRequired || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func cloneRequestForRetry(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.Body == nil {
		return clone, nil
	}
	if req.GetBody == nil {
		return nil, fmt.Errorf("request body cannot be replayed through fallback proxy")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	clone.Body = body
	return clone, nil
}
