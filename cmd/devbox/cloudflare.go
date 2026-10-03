package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// cloudflareAPI is the API base URL; tests point it at a fake server.
var cloudflareAPI = "https://api.cloudflare.com/client/v4"

type cloudflare struct {
	token, account, zone string
	client               *http.Client
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func cloudflareClient() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

func newCloudflare(cfg shareConfig) *cloudflare {
	return &cloudflare{token: cfg.token, account: cfg.AccountID, zone: cfg.ZoneID, client: cloudflareClient()}
}

// call sends one API request and decodes its result into result (if not nil).
func (c *cloudflare) call(method, path string, query url.Values, body, result any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	target := cloudflareAPI + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("Cloudflare %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Success bool            `json:"success"`
		Errors  []apiError      `json:"errors"`
		Result  json.RawMessage `json:"result"`
	}
	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("Cloudflare %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if !envelope.Success {
		messages := []string{}
		for _, e := range envelope.Errors {
			messages = append(messages, fmt.Sprintf("%s (%d)", e.Message, e.Code))
		}
		return fmt.Errorf("Cloudflare %s %s: %s", method, path, strings.Join(messages, "; "))
	}
	if result != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}

type tunnel struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
	DeletedAt *string `json:"deleted_at"`
}

type dnsRecord struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	Comment   string `json:"comment"`
	CreatedOn string `json:"created_on"`
}

func (c *cloudflare) accountPath(format string, args ...any) string {
	return "/accounts/" + url.PathEscape(c.account) + fmt.Sprintf(format, args...)
}

func (c *cloudflare) createTunnel(name string) (tunnel, error) {
	var t tunnel
	err := c.call("POST", c.accountPath("/cfd_tunnel"), nil, map[string]any{"name": name, "config_src": "cloudflare"}, &t)
	return t, err
}

func (c *cloudflare) tunnelToken(id string) (string, error) {
	var token string
	err := c.call("GET", c.accountPath("/cfd_tunnel/%s/token", url.PathEscape(id)), nil, nil, &token)
	return token, err
}

func (c *cloudflare) getTunnel(id string) (tunnel, error) {
	var t tunnel
	err := c.call("GET", c.accountPath("/cfd_tunnel/%s", url.PathEscape(id)), nil, nil, &t)
	return t, err
}

func (c *cloudflare) listTunnels(prefix string) ([]tunnel, error) {
	var tunnels []tunnel
	query := url.Values{"include_prefix": {prefix}, "is_deleted": {"false"}, "per_page": {"1000"}}
	err := c.call("GET", c.accountPath("/cfd_tunnel"), query, nil, &tunnels)
	return tunnels, err
}

// routeTunnel sends every request for host to service, and anything else to a 404.
func (c *cloudflare) routeTunnel(id, host, service string) error {
	body := map[string]any{"config": map[string]any{"ingress": []map[string]any{
		{"hostname": host, "service": service},
		{"service": "http_status:404"},
	}}}
	return c.call("PUT", c.accountPath("/cfd_tunnel/%s/configurations", url.PathEscape(id)), nil, body, nil)
}

// deleteTunnel drops the tunnel's connectors first; Cloudflare refuses to delete a tunnel with connections.
func (c *cloudflare) deleteTunnel(id string) error {
	path := c.accountPath("/cfd_tunnel/%s", url.PathEscape(id))
	if err := c.call("DELETE", path+"/connections", nil, nil, nil); err != nil {
		return err
	}
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = c.call("DELETE", path, nil, nil, nil); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	return err
}

func (c *cloudflare) zoneID(domain string) (string, error) {
	if c.zone != "" {
		return c.zone, nil
	}
	var zones []struct {
		ID string `json:"id"`
	}
	if err := c.call("GET", "/zones", url.Values{"name": {domain}}, nil, &zones); err != nil {
		return "", err
	}
	if len(zones) == 0 {
		return "", fail("Cloudflare has no zone %s for this token; add the domain or set zone_id.", domain)
	}
	c.zone = zones[0].ID
	return c.zone, nil
}

func (c *cloudflare) dnsRecords(query url.Values) ([]dnsRecord, error) {
	var records []dnsRecord
	query.Set("per_page", "1000")
	err := c.call("GET", "/zones/"+url.PathEscape(c.zone)+"/dns_records", query, nil, &records)
	return records, err
}

// pointDNS makes host a proxied CNAME to the tunnel, updating record if it is not empty.
func (c *cloudflare) pointDNS(record, host, tunnelID, comment string) (string, error) {
	body := map[string]any{"type": "CNAME", "name": host, "content": tunnelID + ".cfargotunnel.com",
		"proxied": true, "ttl": 1, "comment": comment}
	var saved dnsRecord
	path := "/zones/" + url.PathEscape(c.zone) + "/dns_records"
	if record == "" {
		return saved.ID, c.call("POST", path, nil, body, &saved)
	}
	err := c.call("PUT", path+"/"+url.PathEscape(record), nil, body, &saved)
	return saved.ID, err
}

func (c *cloudflare) deleteDNS(record string) error {
	return c.call("DELETE", "/zones/"+url.PathEscape(c.zone)+"/dns_records/"+url.PathEscape(record), nil, nil, nil)
}
