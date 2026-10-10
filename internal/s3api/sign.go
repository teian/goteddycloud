// SPDX-License-Identifier: GPL-2.0-only
package s3api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func hmac256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// S3 signs the exact path, including repeated slashes, without normalizing it.
func escape(value string) string {
	const digits = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-_.~", rune(b)) {
			out.WriteByte(b)
		} else {
			out.WriteByte('%')
			out.WriteByte(digits[b>>4])
			out.WriteByte(digits[b&15])
		}
	}
	return out.String()
}
func canonicalQuery(query url.Values) string {
	var parts []string
	for key, values := range query {
		for _, value := range values {
			parts = append(parts, escape(key)+"="+escape(value))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

// Sign implements S3 AWS Signature Version 4 using a hashed seekable payload.
// Payload hashing resets the reader to its original position before sending.
func Sign(req *http.Request, body io.ReadSeeker, credentials Credentials, region string, now time.Time) error {
	payloadHash := hash(nil)
	if body != nil {
		start, err := body.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		h := sha256.New()
		if _, err = io.Copy(h, signingReader{request: req, reader: body}); err != nil {
			return err
		}
		if _, err = body.Seek(start, io.SeekStart); err != nil {
			return err
		}
		payloadHash = hex.EncodeToString(h.Sum(nil))
	}
	if credentials.AccessKey == "" || credentials.SecretKey == "" {
		return errors.New("S3 credentials are required")
	}
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Set("X-Amz-Date", now.UTC().Format("20060102T150405Z"))
	if credentials.Token != "" {
		req.Header.Set("X-Amz-Security-Token", credentials.Token)
	}
	headers := map[string]string{"host": req.URL.Host}
	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "user-agent" || lower == "content-length" {
			continue
		}
		normalized := make([]string, len(values))
		for i, v := range values {
			normalized[i] = strings.Join(strings.Fields(v), " ")
		}
		headers[lower] = strings.Join(normalized, ",")
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + headers[name] + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	canonicalPath := encodePath(req.URL.Path)
	canonical := strings.Join([]string{req.Method, canonicalPath, canonicalQuery(req.URL.Query()), canonicalHeaders.String(), signedHeaders, payloadHash}, "\n")
	date := now.UTC().Format("20060102")
	scope := date + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + now.UTC().Format("20060102T150405Z") + "\n" + scope + "\n" + hash([]byte(canonical))
	key := hmac256([]byte("AWS4"+credentials.SecretKey), date)
	key = hmac256(key, region)
	key = hmac256(key, "s3")
	key = hmac256(key, "aws4_request")
	signature := hex.EncodeToString(hmac256(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+credentials.AccessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return nil
}

func encodePath(value string) string {
	parts := strings.Split(value, "/")
	for i, p := range parts {
		parts[i] = escape(p)
	}
	encoded := strings.Join(parts, "/")
	if encoded == "" {
		return "/"
	}
	return encoded
}

type signingReader struct {
	request *http.Request
	reader  io.Reader
}

func (r signingReader) Read(p []byte) (int, error) {
	if err := r.request.Context().Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
