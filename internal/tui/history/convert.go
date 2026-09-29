package history

import (
	"encoding/base64"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
)

func entryToSendResult(e *history.Entry) tui.SendResult {
	if e == nil {
		return tui.SendResult{}
	}
	res := tui.SendResult{
		RequestID: e.RequestID,
		Started:   e.Time,
	}
	if e.ScriptError != "" {
		res.ScriptError = e.ScriptError
	}
	if e.Error != "" {
		res.Err = staticError(e.Error)
	}
	if e.Response != nil {
		body := []byte(e.Response.Body)
		if e.Response.BodyEncoding == "base64" {
			if b, err := base64.StdEncoding.DecodeString(e.Response.Body); err == nil {
				body = b
			}
		}
		res.Response = &httpx.Response{
			StatusCode:    e.Status,
			Status:        e.StatusText,
			Headers:       append([]model.KV(nil), e.Response.Headers...),
			Body:          body,
			BodyTruncated: e.Response.Truncated,
		}
	}
	return res
}

type staticError string

func (e staticError) Error() string { return string(e) }
