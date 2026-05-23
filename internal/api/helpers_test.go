package api

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
)

func cookiejarNew() (http.CookieJar, error) {
	return cookiejar.New(nil)
}

func mwNew(w io.Writer) *multipart.Writer {
	return multipart.NewWriter(w)
}
