package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// FaviconFor finds the site behind a provider's base URL and keeps its icon
// like a picture picked by hand (#12). The API's own host is asked first,
// then the domains it sits under (api.deepseek.com, then deepseek.com),
// since an API host seldom has a page of its own. On each, the home page's
// <link rel="icon"> pictures come before /favicon.ico. Only public https
// hosts are reached, as for an import link's icon.
func FaviconFor(ctx context.Context, base string) (string, error) {
	sites, err := faviconSites(base)
	if err != nil {
		return "", err
	}
	return favicon(ctx, guardClient(), sites, iconURL)
}

// faviconSites are the https origins to look on for base's icon: its host
// with its port, then each domain above it down to two labels.
func faviconSites(base string) ([]string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("type the provider's base URL first: its site's icon is looked for there")
	}
	host := u.Hostname()
	if local(host) {
		return nil, errors.New("the base URL is on this computer or the local network; its icon can't be fetched")
	}
	sites := []string{"https://" + u.Host}
	if host != u.Host {
		sites = append(sites, "https://"+host)
	}
	if strings.Trim(host, "0123456789.") != "" && !strings.Contains(host, ":") { // not an IP
		labels := strings.Split(host, ".")
		for i := 1; len(labels)-i >= 2; i++ {
			sites = append(sites, "https://"+strings.Join(labels[i:], "."))
		}
	}
	return sites, nil
}

// favicon tries each site in turn; check vets a picture's URL before it is
// fetched (iconURL; tests pass one that lets a local server through).
func favicon(ctx context.Context, c *http.Client, sites []string, check func(string) (string, error)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var last error
	for _, site := range sites {
		cands := append(pageIcons(ctx, c, site), site+"/favicon.ico")
		for _, cand := range cands {
			if data, ok := strings.CutPrefix(cand, "data:"); ok {
				if b := dataURI(data); b != nil {
					if icon, err := StoreIcon(b); err == nil {
						return icon, nil
					}
				}
				continue
			}
			u, err := check(cand)
			if err != nil {
				last = err
				continue
			}
			icon, err := fetchIcon(ctx, c, u)
			if err == nil {
				return icon, nil
			}
			last = err
		}
		if ctx.Err() != nil {
			break
		}
	}
	if last == nil {
		last = errors.New("no icon found")
	}
	return "", errorf("couldn't find the site's icon (%s): %v", strings.Join(sites, ", "), last)
}

// iconUA names magpie to the sites icons are fetched from: some (deepseek.com)
// turn Go's own User-Agent away with a 429.
const iconUA = "magpie"

var (
	linkTag  = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	tagAttr  = regexp.MustCompile(`(?is)\b(rel|href|sizes|type)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	headDone = regexp.MustCompile(`(?i)</head>|<body\b`)
)

// pageIcons are the pictures a site's home page names as its icon, the
// likeliest to look good first: an SVG, then the larger apple-touch-icon,
// then the rest in page order.
func pageIcons(ctx context.Context, c *http.Client, site string) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, site+"/", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", iconUA)
	res, err := c.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(res.Body, 512<<10))
	html := string(b)
	if i := headDone.FindStringIndex(html); i != nil {
		html = html[:i[0]]
	}
	var svg, touch, rest []string
	for _, tag := range linkTag.FindAllString(html, -1) {
		attrs := map[string]string{}
		for _, m := range tagAttr.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = m[2] + m[3] + m[4]
		}
		rel := strings.Fields(strings.ToLower(attrs["rel"]))
		href := strings.TrimSpace(attrs["href"])
		if href == "" {
			continue
		}
		isIcon, isTouch := false, false
		for _, r := range rel {
			switch r {
			case "icon":
				isIcon = true
			case "apple-touch-icon", "apple-touch-icon-precomposed":
				isTouch = true
			}
		}
		if !isIcon && !isTouch {
			continue
		}
		if !strings.HasPrefix(href, "data:") {
			ref, err := url.Parse(href)
			if err != nil {
				continue
			}
			href = res.Request.URL.ResolveReference(ref).String()
		}
		switch {
		case attrs["type"] == "image/svg+xml" || strings.HasSuffix(strings.ToLower(strings.SplitN(href, "?", 2)[0]), ".svg"):
			svg = append(svg, href)
		case isTouch:
			touch = append(touch, href)
		default:
			rest = append(rest, href)
		}
	}
	return append(append(svg, touch...), rest...)
}

// dataURI decodes what follows "data:" in a data URI, nil when it isn't one.
func dataURI(s string) []byte {
	meta, data, ok := strings.Cut(s, ",")
	if !ok {
		return nil
	}
	if strings.HasSuffix(strings.ToLower(meta), ";base64") {
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil
		}
		return b
	}
	d, err := url.PathUnescape(data)
	if err != nil {
		return nil
	}
	return []byte(d)
}
