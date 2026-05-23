package api

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
)

func cookiejarNew() (http.CookieJar, error) {
	return cookiejar.New(nil)
}

func mwNew(w io.Writer) *multipart.Writer {
	return multipart.NewWriter(w)
}

func neturlParse(s string) (*url.URL, error) {
	return url.Parse(s)
}
