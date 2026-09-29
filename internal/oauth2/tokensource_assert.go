package oauth2

import "github.com/shubham/postctl/internal/httpx"

var _ httpx.TokenSource = (*Source)(nil)
