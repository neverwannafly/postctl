package request

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

// parsedCurl is what a pasted curl command means as a request.
type parsedCurl struct {
	Method   string
	URL      string
	Headers  []model.KV
	Body     model.Body
	Auth     *model.Auth
	Insecure bool
	Follow   bool
}

// looksLikeCurl reports whether pasted text is a curl command.
func looksLikeCurl(s string) bool {
	s = strings.TrimSpace(s)
	return s == "curl" || strings.HasPrefix(s, "curl ") || strings.HasPrefix(s, "curl\t") || strings.HasPrefix(s, "curl\\\n")
}

// shellWords splits a command line the way a POSIX shell would, without
// expansion: single quotes are literal, double quotes allow \" \\ \$ \`,
// $'...' understands common escapes, and a backslash-newline continues the
// line.
func shellWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		case r == '\\':
			if i+1 < len(rs) {
				i++
				if rs[i] == '\n' {
					continue // line continuation
				}
				if rs[i] == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
					i++
					continue
				}
				cur.WriteRune(rs[i])
				inWord = true
			}
		case r == '\'':
			inWord = true
			i++
			for i < len(rs) && rs[i] != '\'' {
				cur.WriteRune(rs[i])
				i++
			}
			if i >= len(rs) {
				return nil, errors.New("unterminated single quote")
			}
		case r == '"':
			inWord = true
			i++
			for i < len(rs) && rs[i] != '"' {
				if rs[i] == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`\n", rs[i+1]) {
					i++
					if rs[i] == '\n' {
						i++
						continue
					}
				}
				cur.WriteRune(rs[i])
				i++
			}
			if i >= len(rs) {
				return nil, errors.New("unterminated double quote")
			}
		case r == '$' && i+1 < len(rs) && rs[i+1] == '\'':
			inWord = true
			i += 2
			for i < len(rs) && rs[i] != '\'' {
				if rs[i] == '\\' && i+1 < len(rs) {
					i++
					switch rs[i] {
					case 'n':
						cur.WriteRune('\n')
					case 't':
						cur.WriteRune('\t')
					case 'r':
						cur.WriteRune('\r')
					default:
						cur.WriteRune(rs[i])
					}
				} else {
					cur.WriteRune(rs[i])
				}
				i++
			}
			if i >= len(rs) {
				return nil, errors.New("unterminated $'...' string")
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	flush()
	return words, nil
}

// curlFlags lists the options that take a value, long and short.
var curlValueFlags = map[string]bool{
	"-X": true, "--request": true, "-H": true, "--header": true, "-d": true, "--data": true,
	"--data-raw": true, "--data-binary": true, "--data-ascii": true, "--data-urlencode": true,
	"-u": true, "--user": true, "-F": true, "--form": true, "--url": true, "-A": true, "--user-agent": true,
	"-e": true, "--referer": true, "-b": true, "--cookie": true, "-o": true, "--output": true,
	"-m": true, "--max-time": true, "--connect-timeout": true, "-x": true, "--proxy": true,
	"--cacert": true, "--cert": true, "--key": true, "-w": true, "--write-out": true,
}

// parseCurl turns a curl command line into a request description. It
// understands method, URL, headers, data, basic auth, forms, --compressed,
// -k and redirects; other options are skipped.
func parseCurl(cmd string) (*parsedCurl, error) {
	words, err := shellWords(strings.TrimSpace(cmd))
	if err != nil {
		return nil, err
	}
	if len(words) == 0 || words[0] != "curl" {
		return nil, errors.New("not a curl command")
	}
	pc := &parsedCurl{}
	var data []string
	var forms []model.FormPart
	var rawURL string
	getQuery := false
	head := false
	dataSeen := false

	for i := 1; i < len(words); i++ {
		w, val, hasVal := splitFlag(words[i])
		if !hasVal && curlValueFlags[w] {
			if i+1 >= len(words) {
				return nil, errors.New("option " + w + " needs a value")
			}
			i++
			val = words[i]
		}
		if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && len(w) > 2 {
			// Combined switches such as -sSL.
			for _, c := range w[1:] {
				pc.applySwitch("-"+string(c), &getQuery, &head)
			}
			continue
		}
		switch w {
		case "-X", "--request":
			pc.Method = strings.ToUpper(val)
		case "-H", "--header":
			k, v, ok := strings.Cut(val, ":")
			if !ok {
				if strings.HasSuffix(val, ";") { // "Header;" sends an empty header
					pc.Headers = append(pc.Headers, model.KV{Key: strings.TrimSuffix(val, ";")})
				}
				continue
			}
			pc.Headers = append(pc.Headers, model.KV{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii":
			dataSeen = true
			data = append(data, val)
		case "--data-urlencode":
			dataSeen = true
			if k, v, ok := strings.Cut(val, "="); ok && k != "" {
				val = k + "=" + url.QueryEscape(v)
			} else if ok {
				val = url.QueryEscape(v)
			} else {
				val = url.QueryEscape(val)
			}
			data = append(data, val)
		case "-u", "--user":
			user, pass, _ := strings.Cut(val, ":")
			pc.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: user, Password: pass}}
		case "-F", "--form":
			k, v, _ := strings.Cut(val, "=")
			part := model.FormPart{Key: k, Type: model.FormText, Value: v}
			if strings.HasPrefix(v, "@") {
				part.Type, part.Value, part.Src = model.FormFile, "", strings.TrimPrefix(v, "@")
			}
			forms = append(forms, part)
		case "--url":
			rawURL = val
		case "-A", "--user-agent":
			pc.Headers = append(pc.Headers, model.KV{Key: "User-Agent", Value: val})
		case "-e", "--referer":
			pc.Headers = append(pc.Headers, model.KV{Key: "Referer", Value: val})
		case "-b", "--cookie":
			pc.Headers = append(pc.Headers, model.KV{Key: "Cookie", Value: val})
		case "--compressed", "-k", "--insecure", "-L", "--location", "-G", "--get", "-I", "--head", "-s", "-S", "-v", "-i", "--silent", "--show-error", "--verbose", "--include", "-g", "--globoff":
			pc.applySwitch(w, &getQuery, &head)
		default:
			if !strings.HasPrefix(w, "-") && rawURL == "" {
				rawURL = w
			}
			// Unknown options are skipped; their values (if any) were consumed
			// only for the flags listed in curlValueFlags.
		}
	}
	if rawURL == "" {
		return nil, errors.New("no URL in the curl command")
	}
	pc.URL = rawURL

	ctype := ""
	for _, h := range pc.Headers {
		if strings.EqualFold(h.Key, "Content-Type") {
			ctype = strings.ToLower(h.Value)
		}
	}
	switch {
	case len(forms) > 0:
		pc.Body = model.Body{Mode: model.BodyFormData, FormData: forms}
	case dataSeen && getQuery:
		sep := "?"
		if strings.Contains(pc.URL, "?") {
			sep = "&"
		}
		pc.URL += sep + strings.Join(data, "&")
	case dataSeen:
		joined := strings.Join(data, "&")
		switch {
		case strings.Contains(ctype, "x-www-form-urlencoded"):
			pc.Body = model.Body{Mode: model.BodyURLEncoded, URLEncoded: parseFormPairs(joined)}
		default:
			lang := "text"
			if strings.Contains(ctype, "json") || json.Valid([]byte(joined)) {
				lang = "json"
			} else if strings.Contains(ctype, "xml") {
				lang = "xml"
			}
			pc.Body = model.Body{Mode: model.BodyRaw, Raw: joined, Language: lang}
		}
	}
	if pc.Method == "" {
		switch {
		case head:
			pc.Method = "HEAD"
		case dataSeen && !getQuery, len(forms) > 0:
			pc.Method = "POST"
		default:
			pc.Method = "GET"
		}
	}
	return pc, nil
}

// splitFlag separates "--header=value" and "-XPOST" into flag and value. hasVal
// reports that the value came attached.
func splitFlag(w string) (flag, val string, hasVal bool) {
	if strings.HasPrefix(w, "--") {
		if k, v, ok := strings.Cut(w, "="); ok {
			return k, v, true
		}
		return w, "", false
	}
	if len(w) > 2 && w[0] == '-' && curlValueFlags[w[:2]] {
		return w[:2], w[2:], true
	}
	return w, "", false
}

func (pc *parsedCurl) applySwitch(sw string, getQuery, head *bool) {
	switch sw {
	case "-k", "--insecure":
		pc.Insecure = true
	case "-L", "--location":
		pc.Follow = true
	case "-G", "--get":
		*getQuery = true
	case "-I", "--head":
		*head = true
	}
}

func parseFormPairs(s string) []model.KV {
	var out []model.KV
	for _, pair := range strings.Split(s, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		if dk, err := url.QueryUnescape(k); err == nil {
			k = dk
		}
		if dv, err := url.QueryUnescape(v); err == nil {
			v = dv
		}
		out = append(out, model.KV{Key: k, Value: v})
	}
	return out
}

// summary is a one-line description for the import offer.
func (pc *parsedCurl) summary() string {
	parts := []string{pc.Method + " " + pc.URL}
	if n := len(pc.Headers); n > 0 {
		parts = append(parts, plural(n, "header", "headers"))
	}
	switch pc.Body.Mode {
	case model.BodyRaw:
		parts = append(parts, "raw body")
	case model.BodyURLEncoded:
		parts = append(parts, "form body")
	case model.BodyFormData:
		parts = append(parts, "multipart form")
	}
	if pc.Auth != nil {
		parts = append(parts, "basic auth")
	}
	return strings.Join(parts, " · ")
}

// applyTo replaces the request's method, URL, headers, body and auth with the
// curl command's. Everything else (name, scripts, examples) stays.
func (pc *parsedCurl) applyTo(r *model.Request) {
	r.Method = pc.Method
	r.URL = model.ParseURL(pc.URL)
	r.Headers = append([]model.KV(nil), pc.Headers...)
	r.Body = pc.Body
	if pc.Auth != nil {
		r.Auth = pc.Auth
	}
	if pc.Insecure {
		f := false
		r.Settings.VerifyTLS = &f
	}
	if pc.Follow {
		t := true
		r.Settings.FollowRedirects = &t
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}
