package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// StoredCookie is the persistent representation used by the cookie manager.
type StoredCookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitempty"`
	MaxAge   int       `json:"maxAge,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"httpOnly,omitempty"`
	SameSite int       `json:"sameSite,omitempty"`
	HostOnly bool      `json:"hostOnly,omitempty"`
}

// CookieJar is a public-suffix-aware HTTP cookie jar persisted under the
// workspace's .postctl directory.
type CookieJar struct {
	mu      sync.Mutex
	jar     *cookiejar.Jar
	path    string
	records []StoredCookie
}

// NewCookieJar loads or creates the persistent jar for workspaceDir.
func NewCookieJar(workspaceDir string) (*CookieJar, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	result := &CookieJar{
		jar:  jar,
		path: filepath.Join(workspaceDir, ".postctl", "cookies.json"),
	}
	if err := result.load(); err != nil {
		return nil, err
	}
	return result, nil
}

// Cookies implements http.CookieJar.
func (j *CookieJar) Cookies(target *url.URL) []*http.Cookie {
	if j == nil || target == nil {
		return nil
	}
	return j.jar.Cookies(target)
}

// SetCookies implements http.CookieJar and persists the update.
func (j *CookieJar) SetCookies(target *url.URL, cookies []*http.Cookie) {
	if j == nil || target == nil {
		return
	}
	_ = j.setCookies(target, cookies)
}

func (j *CookieJar) setCookies(target *url.URL, cookies []*http.Cookie) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jar.SetCookies(target, cookies)
	now := time.Now()
	for _, cookie := range cookies {
		j.updateRecord(target, cookie, now)
	}
	j.purgeExpired(now)
	return j.saveLocked()
}

// Get returns cookies that would be sent to rawURL.
func (j *CookieJar) Get(rawURL string) ([]*http.Cookie, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return j.Cookies(target), nil
}

// Set stores one cookie as though it were received from rawURL.
func (j *CookieJar) Set(rawURL string, cookie *http.Cookie) error {
	if j == nil {
		return errors.New("set cookie: nil jar")
	}
	if cookie == nil {
		return errors.New("set cookie: nil cookie")
	}
	target, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	return j.setCookies(target, []*http.Cookie{cookie})
}

// List returns persistent cookie metadata for domain. An empty domain lists
// every cookie. Values are included for the cookie manager and script API.
func (j *CookieJar) List(domain string) []StoredCookie {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.purgeExpired(time.Now())
	domain = canonicalDomain(domain)
	result := make([]StoredCookie, 0, len(j.records))
	for _, record := range j.records {
		if domain == "" || canonicalDomain(record.Domain) == domain {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, k int) bool {
		if result[i].Domain != result[k].Domain {
			return result[i].Domain < result[k].Domain
		}
		if result[i].Path != result[k].Path {
			return result[i].Path < result[k].Path
		}
		return result[i].Name < result[k].Name
	})
	return result
}

// Delete removes a cookie identified by domain, path and name.
func (j *CookieJar) Delete(domain, path, name string) error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	domain = canonicalDomain(domain)
	if path == "" {
		path = "/"
	}
	kept := j.records[:0]
	for _, record := range j.records {
		if canonicalDomain(record.Domain) == domain && record.Path == path && record.Name == name {
			target := cookieURL(record)
			j.jar.SetCookies(target, []*http.Cookie{
				{Name: record.Name, Value: "", Path: record.Path, MaxAge: -1},
			})
			continue
		}
		kept = append(kept, record)
	}
	j.records = kept
	return j.saveLocked()
}

// Clear removes every cookie for domain. An empty domain clears the jar.
func (j *CookieJar) Clear(domain string) error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	domain = canonicalDomain(domain)
	kept := make([]StoredCookie, 0, len(j.records))
	for _, record := range j.records {
		if domain != "" && canonicalDomain(record.Domain) != domain {
			kept = append(kept, record)
			continue
		}
		target := cookieURL(record)
		j.jar.SetCookies(target, []*http.Cookie{
			{Name: record.Name, Value: "", Path: record.Path, MaxAge: -1},
		})
	}
	j.records = kept
	return j.saveLocked()
}

func (j *CookieJar) load() error {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &j.records); err != nil {
		return err
	}
	now := time.Now()
	j.purgeExpired(now)
	for _, record := range j.records {
		cookie := &http.Cookie{
			Name:     record.Name,
			Value:    record.Value,
			Path:     record.Path,
			Expires:  record.Expires,
			MaxAge:   record.MaxAge,
			Secure:   record.Secure,
			HttpOnly: record.HTTPOnly,
			SameSite: http.SameSite(record.SameSite),
		}
		if !record.HostOnly {
			cookie.Domain = record.Domain
		}
		j.jar.SetCookies(cookieURL(record), []*http.Cookie{cookie})
	}
	return nil
}

func (j *CookieJar) updateRecord(target *url.URL, cookie *http.Cookie, now time.Time) {
	domain := canonicalDomain(cookie.Domain)
	hostOnly := domain == ""
	if hostOnly {
		domain = canonicalDomain(target.Hostname())
	}
	path := cookie.Path
	if path == "" {
		path = defaultCookiePath(target.Path)
	}
	deleteCookie := cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && !cookie.Expires.After(now))
	index := -1
	for i := range j.records {
		record := &j.records[i]
		if canonicalDomain(record.Domain) == domain && record.Path == path && record.Name == cookie.Name {
			index = i
			break
		}
	}
	if deleteCookie {
		if index >= 0 {
			j.records = append(j.records[:index], j.records[index+1:]...)
		}
		return
	}
	record := StoredCookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Domain:   domain,
		Path:     path,
		Expires:  cookie.Expires,
		MaxAge:   cookie.MaxAge,
		Secure:   cookie.Secure,
		HTTPOnly: cookie.HttpOnly,
		SameSite: int(cookie.SameSite),
		HostOnly: hostOnly,
	}
	if index >= 0 {
		j.records[index] = record
	} else {
		j.records = append(j.records, record)
	}
}

func (j *CookieJar) purgeExpired(now time.Time) {
	kept := j.records[:0]
	for _, record := range j.records {
		if !record.Expires.IsZero() && !record.Expires.After(now) {
			continue
		}
		kept = append(kept, record)
	}
	j.records = kept
}

func (j *CookieJar) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(j.records, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(j.path), ".cookies-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, j.path)
}

func cookieURL(cookie StoredCookie) *url.URL {
	scheme := "http"
	if cookie.Secure {
		scheme = "https"
	}
	return &url.URL{Scheme: scheme, Host: canonicalDomain(cookie.Domain), Path: cookie.Path}
}

func canonicalDomain(domain string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
}

func defaultCookiePath(requestPath string) string {
	if requestPath == "" || requestPath[0] != '/' {
		return "/"
	}
	lastSlash := strings.LastIndex(requestPath, "/")
	if lastSlash <= 0 {
		return "/"
	}
	return requestPath[:lastSlash]
}
