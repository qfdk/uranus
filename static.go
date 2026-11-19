package main

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// staticFiles serves web/public under /public/. Text files are sent gzipped to
// browsers that accept it (the terminal's ghostty-web shrinks from 680 KB to
// 190 KB); each file is compressed once.
func staticFiles(fsys fs.FS) gin.HandlerFunc {
	var gzipped sync.Map // file name -> compressed bytes
	return func(c *gin.Context) {
		rest, ok := strings.CutPrefix(c.Request.URL.Path, "/public/")
		if !ok {
			c.Next()
			return
		}
		name := path.Join("web/public", rest)
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			c.Next()
			return
		}

		contentType := getMimeType(name)
		if compressible(contentType) && len(data) > 1024 {
			c.Header("Vary", "Accept-Encoding")
			if strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
				gz, ok := gzipped.Load(name)
				if !ok {
					var buf bytes.Buffer
					w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
					w.Write(data)
					w.Close()
					gz, _ = gzipped.LoadOrStore(name, buf.Bytes())
				}
				c.Header("Content-Encoding", "gzip")
				data = gz.([]byte)
			}
		}
		c.Data(http.StatusOK, contentType, data)
		c.Abort()
	}
}

func compressible(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") ||
		contentType == "application/javascript" || contentType == "image/svg+xml"
}
