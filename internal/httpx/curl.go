package httpx

import (
	"net/url"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

// Curl renders a model request as a shell-safe curl command.
func Curl(req *model.Request) string {
	if req == nil {
		return "curl"
	}
	arguments := []string{"curl", "--request", shellQuote(req.Method), "--url", shellQuote(req.URL.Raw)}
	for _, header := range req.Headers {
		if !header.Disabled {
			arguments = append(arguments, "--header", shellQuote(header.Key+": "+header.Value))
		}
	}
	if req.Body.Disabled {
		return strings.Join(arguments, " ")
	}
	switch req.Body.Mode {
	case model.BodyRaw:
		arguments = append(arguments, "--data-raw", shellQuote(req.Body.Raw))
	case model.BodyURLEncoded:
		values := make(url.Values)
		for _, field := range req.Body.URLEncoded {
			if field.Enabled() {
				values.Add(field.Key, field.Value)
			}
		}
		arguments = append(arguments, "--data", shellQuote(values.Encode()))
	case model.BodyFormData:
		for _, part := range req.Body.FormData {
			if part.Disabled {
				continue
			}
			value := part.Key + "=" + part.Value
			if part.Type == model.FormFile {
				value = part.Key + "=@" + part.Src
			}
			arguments = append(arguments, "--form", shellQuote(value))
		}
	case model.BodyBinary:
		arguments = append(arguments, "--data-binary", shellQuote("@"+req.Body.File))
	case model.BodyGraphQL:
		if req.Body.GraphQL != nil {
			body, err := buildGraphQL(req.Body.GraphQL)
			if err == nil {
				arguments = append(arguments, "--header", shellQuote("Content-Type: application/json"))
				arguments = append(arguments, "--data-raw", shellQuote(string(body.data)))
			}
		}
	}
	return strings.Join(arguments, " ")
}

// CurlFromSent renders the exact sent method, URL, headers and body as a
// shell-safe curl command.
func CurlFromSent(sent SentRequest) string {
	arguments := []string{"curl", "--request", shellQuote(sent.Method), "--url", shellQuote(sent.URL)}
	for _, header := range sent.Headers {
		if !header.Disabled {
			arguments = append(arguments, "--header", shellQuote(header.Key+": "+header.Value))
		}
	}
	if sent.Body != nil {
		arguments = append(arguments, "--data-binary", shellQuote(string(sent.Body)))
	}
	return strings.Join(arguments, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
