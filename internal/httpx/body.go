package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

type builtBody struct {
	data        []byte
	contentType string
	open        func() (io.ReadCloser, error)
	size        int64
}

func buildBody(req *model.Request, workspaceDir string, sendBody bool) (builtBody, error) {
	if !sendBody || req.Body.Disabled || req.Body.Mode == "" || req.Body.Mode == model.BodyNone {
		return builtBody{}, nil
	}
	switch req.Body.Mode {
	case model.BodyRaw:
		return builtBody{
			data:        []byte(req.Body.Raw),
			contentType: rawContentType(req.Body.Language),
		}, nil
	case model.BodyURLEncoded:
		values := make(url.Values)
		for _, field := range req.Body.URLEncoded {
			if field.Enabled() {
				values.Add(field.Key, field.Value)
			}
		}
		return builtBody{
			data:        []byte(values.Encode()),
			contentType: "application/x-www-form-urlencoded",
		}, nil
	case model.BodyFormData:
		return buildMultipart(req.Body.FormData, workspaceDir)
	case model.BodyBinary:
		path := resolveFile(workspaceDir, req.Body.File)
		file, err := os.Open(path)
		if err != nil {
			return builtBody{}, fmt.Errorf("read binary body: %w", err)
		}
		sample := make([]byte, 512)
		n, readErr := file.Read(sample)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			_ = file.Close()
			return builtBody{}, fmt.Errorf("read binary body: %w", readErr)
		}
		info, err := file.Stat()
		_ = file.Close()
		if err != nil {
			return builtBody{}, fmt.Errorf("stat binary body: %w", err)
		}
		return builtBody{
			contentType: http.DetectContentType(sample[:n]),
			open:        func() (io.ReadCloser, error) { return os.Open(path) },
			size:        info.Size(),
		}, nil
	case model.BodyGraphQL:
		return buildGraphQL(req.Body.GraphQL)
	default:
		return builtBody{}, fmt.Errorf("unsupported body mode %q", req.Body.Mode)
	}
}

func (body builtBody) authData(auth *model.Auth) ([]byte, error) {
	if body.data != nil || body.open == nil || auth == nil {
		return body.data, nil
	}
	switch auth.Type {
	case model.AuthDigest, model.AuthAWSV4, model.AuthHawk:
		reader, err := body.open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = reader.Close() }()
		return io.ReadAll(reader)
	default:
		return nil, nil
	}
}

func buildMultipart(parts []model.FormPart, workspaceDir string) (builtBody, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, part := range parts {
		if part.Disabled {
			continue
		}
		if part.Type == model.FormFile {
			path := resolveFile(workspaceDir, part.Src)
			data, err := os.ReadFile(path)
			if err != nil {
				return builtBody{}, fmt.Errorf("read multipart file %q: %w", part.Src, err)
			}
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition",
				`form-data; name="`+escapeQuotes(part.Key)+`"; filename="`+
					escapeQuotes(filepath.Base(path))+`"`)
			contentType := part.ContentType
			if contentType == "" {
				contentType = http.DetectContentType(data)
			}
			header.Set("Content-Type", contentType)
			filePart, err := writer.CreatePart(header)
			if err != nil {
				return builtBody{}, err
			}
			if _, err := filePart.Write(data); err != nil {
				return builtBody{}, err
			}
			continue
		}
		field, err := writer.CreateFormField(part.Key)
		if err != nil {
			return builtBody{}, err
		}
		if _, err := field.Write([]byte(part.Value)); err != nil {
			return builtBody{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return builtBody{}, err
	}
	return builtBody{data: buffer.Bytes(), contentType: writer.FormDataContentType()}, nil
}

func buildGraphQL(graphql *model.GraphQLBody) (builtBody, error) {
	if graphql == nil {
		return builtBody{}, errors.New("graphql body is missing")
	}
	var variables any = map[string]any{}
	if strings.TrimSpace(graphql.Variables) != "" {
		if err := json.Unmarshal([]byte(graphql.Variables), &variables); err != nil {
			return builtBody{}, fmt.Errorf("decode GraphQL variables: %w", err)
		}
	}
	data, err := json.Marshal(struct {
		Query     string `json:"query"`
		Variables any    `json:"variables"`
	}{
		Query:     graphql.Query,
		Variables: variables,
	})
	if err != nil {
		return builtBody{}, err
	}
	return builtBody{data: data, contentType: "application/json"}, nil
}

func rawContentType(language string) string {
	switch strings.ToLower(language) {
	case "json":
		return "application/json"
	case "xml":
		return "application/xml"
	case "html":
		return "text/html"
	case "javascript", "js":
		return "application/javascript"
	case "text", "":
		return "text/plain"
	default:
		return ""
	}
}

func resolveFile(workspaceDir, name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(workspaceDir, name)
}

func escapeQuotes(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`)
}
