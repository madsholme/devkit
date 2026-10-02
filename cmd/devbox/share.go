package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A public share is a cloudflared container next to a preview. Its labels are the only record of it:
// the preview it belongs to, the public hostname, the Cloudflare tunnel and DNS record, and when it expires.
// Tunnels are named devbox-<machine>-<preview> and DNS records carry the comment "devbox share <machine>",
// so clean-up only ever touches this machine's resources, even when several hosts share one account.

type share struct {
	Preview    string
	Container  string
	Host       string
	URL        string
	Tunnel     string
	DNS        string
	Expires    time.Time
	Fixed      bool
	Repository string
}

func shareOf(c container) (share, bool) {
	l := c.Config.Labels
	if l["devbox.service"] != "share" {
		return share{}, false
	}
	expires, _ := time.Parse(time.RFC3339, l["devbox.share.expires"])
	return share{Preview: l["devbox.preview"], Container: c.ID, Host: l["devbox.share.host"], URL: l["devbox.share.url"],
		Tunnel: l["devbox.share.tunnel"], DNS: l["devbox.share.dns"], Expires: expires,
		Fixed: l["devbox.share.fixed"] == "true", Repository: l["devbox.repository"]}, true
}

func allShares() ([]share, error) {
	containers, err := dockerContainers()
	var shares []share
	for _, c := range containers {
		if s, ok := shareOf(c); ok {
			shares = append(shares, s)
		}
	}
	return shares, err
}

func shareFor(preview string) (share, bool, error) {
	shares, err := allShares()
	for _, s := range shares {
		if s.Preview == preview {
			return s, true, err
		}
	}
	return share{}, false, err
}

// machine names this host in Cloudflare resource names and comments.
var machine = func() string {
	name, _ := os.Hostname()
	return slug(name, 20)
}

func tunnelPrefix() string { return "devbox-" + machine() + "-" }
func dnsComment() string   { return "devbox share " + machine() }

// shareName is the DNS label of a preview's share: prefix + preview ID, shortened with a digest
// when that would pass the 63-character limit of a DNS label.
func shareName(prefix, preview string) string {
	name := prefix + preview
	if len(name) <= 63 {
		return name
	}
	return strings.TrimRight(name[:63-clash], "-") + "-" + digest(preview)[:clash-1]
}

// shareTarget returns a preview's running web container, its name and the port Traefik routes to.
func shareTarget(preview string) (container, string, error) {
	containers, err := containersFor(preview)
	if err != nil {
		return container{}, "", err
	}
	for _, c := range containers {
		if !isWeb(c.Config.Labels) {
			continue
		}
		if !c.State.Running {
			return c, "", fail("Preview %s is not running; start it first.", preview)
		}
		for _, p := range containerPorts(c) {
			if p.Routed {
				return c, fmt.Sprintf("http://%s:%d", strings.TrimPrefix(c.Name, "/"), p.Port), nil
			}
		}
		return c, "", fail("Preview %s has no routed web port.", preview)
	}
	return container{}, "", fail("No preview %s on this host (see devbox ls).", preview)
}

// setPublicURL records DEVBOX_PUBLIC_URL in the preview's web service ("" removes it) and, if apply,
// recreates the web container so the app sees it.
var setPublicURL = func(common, preview, publicURL string, apply bool) error {
	path := filepath.Join(runDir(common, preview), "compose.json")
	project, err := readCompose(path)
	if err != nil {
		return err
	}
	changed := false
	for _, s := range object(project, "services") {
		service := asObject(s)
		labels := map[string]string{}
		for k, v := range object(service, "labels") {
			labels[k], _ = v.(string)
		}
		if !isWeb(labels) {
			continue
		}
		env := ensureObject(service, "environment")
		if publicURL == "" {
			_, changed = env["DEVBOX_PUBLIC_URL"]
			delete(env, "DEVBOX_PUBLIC_URL")
		} else {
			changed = env["DEVBOX_PUBLIC_URL"] != publicURL
			env["DEVBOX_PUBLIC_URL"] = publicURL
		}
	}
	if !changed {
		return nil
	}
	if err := writeCompose(path, project); err != nil {
		return err
	}
	if apply {
		return up(path)
	}
	return nil
}

func shareLabels(web container, s share) []string {
	labels := map[string]string{
		managedLabel: "true", "devbox.service": "share", "devbox.share.host": s.Host, "devbox.share.url": s.URL,
		"devbox.share.tunnel": s.Tunnel, "devbox.share.dns": s.DNS, "devbox.share.fixed": fmt.Sprint(s.Fixed),
		"devbox.share.expires": s.Expires.UTC().Format(time.RFC3339),
	}
	// The share carries its preview's labels, so it groups with the preview wherever containers are listed.
	for _, key := range []string{"devbox.preview", "devbox.mode", "devbox.project", "devbox.repository", "devbox.worktree",
		"devbox.branch", "devbox.commit", "devbox.identity", "devbox.url"} {
		labels[key] = web.Config.Labels[key]
	}
	args := []string{}
	for _, key := range sortedKeys(labels) {
		args = append(args, "--label", key+"="+labels[key])
	}
	return args
}

// startCloudflared runs the connector; the token travels in the environment, not on the command line.
var startCloudflared = func(cfg shareConfig, web container, s share, token string) (string, error) {
	args := append([]string{"docker", "run", "-d", "--quiet", "--name", s.Preview + "-share", "--network", "devbox-proxy",
		"--restart", "unless-stopped", "--env", "TUNNEL_TOKEN"}, shareLabels(web, s)...)
	args = append(args, cfg.Image, "tunnel", "--no-autoupdate", "run")
	return runCommand(args, runOpts{capture: true, env: withEnv(os.Environ(), map[string]string{"TUNNEL_TOKEN": token})})
}

var removeContainer = func(id string) error {
	_, err := runCommand([]string{"docker", "rm", "-f", id}, runOpts{capture: true})
	return err
}

// tunnelWait is how long share waits for cloudflared to connect.
var tunnelWait = 45 * time.Second

func waitHealthy(cf *cloudflare, id string) bool {
	deadline := time.Now().Add(tunnelWait)
	for {
		if t, err := cf.getTunnel(id); err == nil && t.Status == "healthy" {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

// resolvePreview returns the named preview, or the current worktree's one when name is empty.
func resolvePreview(name string) (string, error) {
	if name != "" {
		if !previewID.MatchString(name) {
			return "", fail("Use an exact preview ID from devbox ls.")
		}
		return name, nil
	}
	root, common, err := repository()
	if err != nil {
		return "", err
	}
	return currentPreview(root, common)
}

// sharePreview makes a running preview public at <preview>.<domain>, or at a fixed host from the config.
func sharePreview(name, fixedHost string, hours int) error {
	cfg, err := requireShareConfig()
	if err != nil {
		return err
	}
	if name, err = resolvePreview(name); err != nil {
		return err
	}
	if hours == 0 {
		hours = cfg.DefaultHours
	}
	if hours < 1 || hours > cfg.MaxHours {
		return fail("--hours must be between 1 and %d (max_hours in the host config).", cfg.MaxHours)
	}
	label := shareName(*cfg.Prefix, name)
	if fixedHost != "" {
		known := false
		for _, h := range cfg.Hosts {
			known = known || h == fixedHost
		}
		if !known {
			return fail("%s is not one of the configured share hosts: %s.", fixedHost, strings.Join(cfg.Hosts, ", "))
		}
		label = fixedHost
	}
	host := label + "." + cfg.Domain
	web, service, err := shareTarget(name)
	if err != nil {
		return err
	}
	shares, err := allShares()
	if err != nil {
		return err
	}
	expires := time.Now().Add(time.Duration(hours) * time.Hour).UTC().Truncate(time.Second)
	cf := newCloudflare(cfg)
	for _, s := range shares {
		if s.Preview == name {
			if s.Host != host {
				return fail("%s is already shared at %s; run devbox unshare first.", name, s.URL)
			}
			return extendShare(cfg, cf, web, s, expires)
		}
		if s.Host == host {
			return fail("%s is in use by the share of %s.", host, s.Preview)
		}
	}
	if _, err := cf.zoneID(cfg.Domain); err != nil {
		return err
	}
	records, err := cf.dnsRecords(url.Values{"name": {host}})
	if err != nil {
		return err
	}
	record := ""
	for _, r := range records {
		// A leftover of this machine's earlier shares may be reused; anything else is not ours to change.
		if r.Type != "CNAME" || r.Comment != dnsComment() || record != "" {
			return fail("%s already has a DNS record that devbox on this machine did not create.", host)
		}
		record = r.ID
	}
	s := share{Preview: name, Host: host, URL: "https://" + host, Expires: expires, Fixed: fixedHost != "",
		Repository: web.Config.Labels["devbox.repository"]}
	var undo []func()
	rollback := func(err error) error {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return err
	}
	t, err := cf.createTunnel(tunnelPrefix() + name)
	if err != nil {
		return err
	}
	s.Tunnel = t.ID
	undo = append(undo, func() { cf.deleteTunnel(t.ID) })
	if err := cf.routeTunnel(t.ID, host, service); err != nil {
		return rollback(err)
	}
	if s.DNS, err = cf.pointDNS(record, host, t.ID, dnsComment()); err != nil {
		return rollback(err)
	}
	undo = append(undo, func() { cf.deleteDNS(s.DNS) })
	token, err := cf.tunnelToken(t.ID)
	if err != nil {
		return rollback(err)
	}
	if err := setPublicURL(s.Repository, name, s.URL, true); err != nil {
		return rollback(err)
	}
	undo = append(undo, func() { setPublicURL(s.Repository, name, "", true) })
	// Recreating the web service kept its container name, so the tunnel's target is unchanged.
	id, err := startCloudflared(cfg, web, s, token)
	if err != nil {
		return rollback(err)
	}
	if !waitHealthy(cf, t.ID) {
		fmt.Fprintf(os.Stderr, "The tunnel has not connected yet; check docker logs %s-share.\n", name)
	}
	fmt.Fprintln(stdout, s.URL)
	fmt.Fprintf(stdout, "share=%s preview=%s expires=%s container=%s\n", host, name, expires.Format(time.RFC3339), shortID(id))
	happened("share", name, s.URL+" until "+localTime(expires))
	return nil
}

func shortID(id string) string { return id[:min(12, len(id))] }

func localTime(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

// extendShare gives an existing share a new expiry. Labels cannot change on a running container,
// so the connector is replaced; the tunnel, DNS record and URL stay the same.
func extendShare(cfg shareConfig, cf *cloudflare, web container, s share, expires time.Time) error {
	token, err := cf.tunnelToken(s.Tunnel)
	if err != nil {
		return err
	}
	if err := removeContainer(s.Container); err != nil {
		return err
	}
	s.Expires = expires
	if _, err := startCloudflared(cfg, web, s, token); err != nil {
		return err
	}
	fmt.Fprintln(stdout, s.URL)
	fmt.Fprintf(stdout, "share=%s preview=%s expires=%s (extended)\n", s.Host, s.Preview, expires.Format(time.RFC3339))
	happened("extend", s.Preview, s.URL+" until "+localTime(expires))
	return nil
}

// removeShare deletes the connector, the app's public URL, the DNS record and the tunnel.
// restartApp recreates a running web container without DEVBOX_PUBLIC_URL; stop and destroy skip that.
func removeShare(s share, restartApp bool) error {
	var problems []string
	if err := removeContainer(s.Container); err != nil {
		problems = append(problems, err.Error())
	}
	if s.Repository != "" {
		if err := setPublicURL(s.Repository, s.Preview, "", restartApp); err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, err.Error())
		}
	}
	cfg, ok, err := loadShareConfig()
	switch {
	case err != nil:
		problems = append(problems, err.Error())
	case !ok:
		problems = append(problems, "sharing is not configured, so the Cloudflare tunnel and DNS record were left in place")
	default:
		cf := newCloudflare(cfg)
		if _, err := cf.zoneID(cfg.Domain); err != nil {
			problems = append(problems, err.Error())
		} else if s.DNS != "" {
			if err := cf.deleteDNS(s.DNS); err != nil {
				problems = append(problems, err.Error())
			}
		}
		if s.Tunnel != "" {
			if err := cf.deleteTunnel(s.Tunnel); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("unsharing %s: %s", s.Preview, strings.Join(problems, "; "))
	}
	return nil
}

// unshareIfShared removes a preview's share before it is stopped or destroyed.
var unshareIfShared = func(preview string) error {
	s, ok, err := shareFor(preview)
	if err != nil || !ok {
		return err
	}
	if err := removeShare(s, false); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "unshared: %s\n", s.URL)
	happened("unshare", preview, s.URL)
	return nil
}

func unsharePreview(name string) error {
	name, err := resolvePreview(name)
	if err != nil {
		return err
	}
	s, ok, err := shareFor(name)
	if err != nil {
		return err
	}
	if !ok {
		return fail("%s is not shared.", name)
	}
	if err := removeShare(s, true); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "unshared: %s\n", s.URL)
	happened("unshare", name, s.URL)
	return nil
}

// reapExpired removes every share whose time is up; it returns what it removed.
func reapExpired(now time.Time) ([]string, error) {
	shares, err := allShares()
	if err != nil {
		return nil, err
	}
	var removed []string
	var problems []string
	for _, s := range shares {
		if s.Expires.IsZero() || now.Before(s.Expires) {
			continue
		}
		if err := removeShare(s, true); err != nil {
			problems = append(problems, err.Error())
			failed("expire", s.Preview, err)
			continue
		}
		removed = append(removed, s.URL)
		happened("expire", s.Preview, s.URL)
	}
	if len(problems) > 0 {
		return removed, errors.New(strings.Join(problems, "; "))
	}
	return removed, nil
}

// sweepOrphans deletes this machine's tunnels and DNS records that no share container uses any more,
// e.g. after a crash halfway through share. Anything younger than grace is left alone, so a share
// that is being created right now is never touched.
func sweepOrphans(cfg shareConfig, grace time.Duration) ([]string, error) {
	shares, err := allShares()
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, s := range shares {
		live[s.Tunnel] = true
	}
	old := func(created string) bool {
		t, err := time.Parse(time.RFC3339, created)
		return err == nil && time.Since(t) > grace
	}
	cf := newCloudflare(cfg)
	if _, err := cf.zoneID(cfg.Domain); err != nil {
		return nil, err
	}
	var removed []string
	tunnels, err := cf.listTunnels(tunnelPrefix())
	if err != nil {
		return nil, err
	}
	for _, t := range tunnels {
		if !live[t.ID] && strings.HasPrefix(t.Name, tunnelPrefix()) && t.DeletedAt == nil && old(t.CreatedAt) {
			if err := cf.deleteTunnel(t.ID); err == nil {
				removed = append(removed, "tunnel "+t.Name)
				happened("sweep", "", "tunnel "+t.Name)
			}
		}
	}
	records, err := cf.dnsRecords(url.Values{"type": {"CNAME"}, "comment.exact": {dnsComment()}})
	if err != nil {
		return removed, err
	}
	for _, r := range records {
		tunnelID := strings.TrimSuffix(r.Content, ".cfargotunnel.com")
		if r.Comment == dnsComment() && !live[tunnelID] && old(r.CreatedOn) {
			if err := cf.deleteDNS(r.ID); err == nil {
				removed = append(removed, "DNS "+r.Name)
				happened("sweep", "", "DNS record "+r.Name)
			}
		}
	}
	return removed, nil
}

func listShares() error {
	shares, err := allShares()
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "PREVIEW\tURL\tEXPIRES\tHOST")
	for _, s := range shares {
		kind := "preview"
		if s.Fixed {
			kind = "fixed"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", s.Preview, s.URL, s.Expires.Local().Format("2006-01-02 15:04"), kind)
	}
	return nil
}

// cleanShares is devbox unshare --expired: remove expired shares, then sweep leftovers.
func cleanShares() error {
	removed, err := reapExpired(time.Now())
	for _, r := range removed {
		fmt.Fprintf(stdout, "unshared (expired): %s\n", r)
	}
	if err != nil {
		return err
	}
	cfg, ok, err := loadShareConfig()
	if err != nil || !ok {
		return err
	}
	swept, err := sweepOrphans(cfg, 10*time.Minute)
	for _, r := range swept {
		fmt.Fprintf(stdout, "removed leftover %s\n", r)
	}
	return err
}
