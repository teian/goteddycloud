// SPDX-License-Identifier: GPL-2.0-only
// Package s3api implements the bounded S3 REST operations needed by deployment
// storage. It deliberately has no dependency on a vendor SDK.
package s3api

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Endpoint, Region, Bucket string
	PathStyle                bool
}
type Client struct {
	options     Options
	endpoint    *url.URL
	http        *http.Client
	credentials Credentials
}

func New(options Options) (*Client, error) {
	if options.Bucket == "" || strings.ContainsAny(options.Bucket, "/\\:@?# \t\r\n") {
		return nil, errors.New("invalid S3 bucket")
	}
	if options.Region == "" {
		options.Region = "us-east-1"
	}
	if options.Endpoint == "" {
		options.Endpoint = "https://s3." + options.Region + ".amazonaws.com"
	}
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return nil, errors.New("invalid S3 endpoint")
	}
	credentials, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	return &Client{options: options, endpoint: endpoint, credentials: credentials, http: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string {
	return fmt.Sprintf("S3 request failed (%s, HTTP %d)", e.Code, e.Status)
}

func (c *Client) request(ctx context.Context, method, key string, query url.Values, headers http.Header, body io.ReadSeeker, length int64) (*http.Response, error) {
	target := *c.endpoint
	target.Path = strings.TrimSuffix(target.Path, "/") + "/"
	if c.options.PathStyle {
		target.Path += c.options.Bucket + "/"
	} else {
		target.Host = c.options.Bucket + "." + target.Host
	}
	target.Path += key
	target.RawPath = encodePath(target.Path)
	target.RawQuery = canonicalQuery(query)
	var reader io.Reader
	if body != nil && length > 0 {
		reader = struct{ io.Reader }{body}
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, errors.New("invalid S3 request")
	}
	req.ContentLength = length
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if err = Sign(req, body, c.credentials, c.options.Region, time.Now().UTC()); err != nil {
		return nil, err
	}
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("S3 transport failure")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	defer response.Body.Close()
	var failure struct{ Code string }
	_ = xml.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&failure)
	// Do not include provider messages, URLs, request IDs or credentials in errors.
	if failure.Code == "" {
		failure.Code = http.StatusText(response.StatusCode)
	}
	return nil, &Error{Code: failure.Code, Status: response.StatusCode}
}

type Object struct {
	Key           string
	Size          int64
	Modified      time.Time
	ETag, Version string
	Metadata      map[string]string
}

func (c *Client) Head(ctx context.Context, key string) (Object, error) {
	response, err := c.request(ctx, "HEAD", key, nil, nil, nil, 0)
	if err != nil {
		return Object{}, err
	}
	defer response.Body.Close()
	size, err := strconv.ParseInt(response.Header.Get("Content-Length"), 10, 64)
	if err != nil || size < 0 {
		return Object{}, errors.New("invalid S3 object length")
	}
	modified, _ := http.ParseTime(response.Header.Get("Last-Modified"))
	metadata := map[string]string{}
	for name, values := range response.Header {
		if suffix, ok := strings.CutPrefix(strings.ToLower(name), "x-amz-meta-"); ok && len(values) > 0 {
			metadata[suffix] = values[0]
		}
	}
	etag := response.Header.Get("ETag")
	if etag == "" {
		return Object{}, errors.New("S3 object is missing an ETag")
	}
	return Object{Key: key, Size: size, Modified: modified, ETag: etag, Version: response.Header.Get("X-Amz-Version-Id"), Metadata: metadata}, nil
}
func (c *Client) Get(ctx context.Context, key, etag, version, byteRange string) (io.ReadCloser, error) {
	headers := make(http.Header)
	query := make(url.Values)
	if etag != "" {
		headers.Set("If-Match", etag)
	}
	if version != "" {
		query.Set("versionId", version)
	}
	if byteRange != "" {
		headers.Set("Range", byteRange)
	}
	response, err := c.request(ctx, "GET", key, query, headers, nil, 0)
	if err != nil {
		return nil, err
	}
	if etag != "" && response.Header.Get("ETag") != etag {
		response.Body.Close()
		return nil, errors.New("S3 object changed during read")
	}
	if byteRange != "" && response.StatusCode != http.StatusPartialContent {
		response.Body.Close()
		return nil, errors.New("S3 endpoint ignored byte range")
	}
	if byteRange != "" {
		var start, end, gotStart, gotEnd, total int64
		_, expectedErr := fmt.Sscanf(byteRange, "bytes=%d-%d", &start, &end)
		_, rangeErr := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &gotStart, &gotEnd, &total)
		if expectedErr != nil || rangeErr != nil || start != gotStart || end != gotEnd || total <= end {
			response.Body.Close()
			return nil, errors.New("invalid S3 range response")
		}
	}
	return response.Body, nil
}
func (c *Client) Put(ctx context.Context, key string, body io.ReadSeeker, length int64, metadata map[string]string, exclusive bool) error {
	headers := make(http.Header)
	for key, value := range metadata {
		headers.Set("X-Amz-Meta-"+key, value)
	}
	if exclusive {
		headers.Set("If-None-Match", "*")
	}
	response, err := c.request(ctx, "PUT", key, nil, headers, body, length)
	if err != nil {
		return err
	}
	return response.Body.Close()
}
func (c *Client) Delete(ctx context.Context, key string) error {
	response, err := c.request(ctx, "DELETE", key, nil, nil, nil, 0)
	if err != nil {
		return err
	}
	return response.Body.Close()
}
func (c *Client) Copy(ctx context.Context, source, target, etag string) error {
	headers := make(http.Header)
	headers.Set("X-Amz-Copy-Source", (&url.URL{Path: c.options.Bucket + "/" + source}).EscapedPath())
	if etag != "" {
		headers.Set("X-Amz-Copy-Source-If-Match", etag)
	}
	response, err := c.request(ctx, "PUT", target, nil, headers, nil, 0)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	// CopyObject may return an embedded error with HTTP 200.
	var result struct {
		XMLName xml.Name
		Code    string
	}
	if err = xml.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return errors.New("invalid S3 copy response")
	}
	if result.XMLName.Local != "CopyObjectResult" {
		return &Error{Code: result.Code, Status: response.StatusCode}
	}
	return nil
}

type Page struct {
	Objects   []Object
	Prefixes  []string
	NextToken string
}

func (c *Client) List(ctx context.Context, prefix, delimiter, token string, maxKeys int) (Page, error) {
	query := url.Values{"list-type": {"2"}, "prefix": {prefix}}
	if delimiter != "" {
		query.Set("delimiter", delimiter)
	}
	if token != "" {
		query.Set("continuation-token", token)
	}
	if maxKeys > 0 {
		query.Set("max-keys", strconv.Itoa(maxKeys))
	}
	response, err := c.request(ctx, "GET", "", query, nil, nil, 0)
	if err != nil {
		return Page{}, err
	}
	defer response.Body.Close()
	var result struct {
		XMLName   xml.Name
		Truncated bool   `xml:"IsTruncated"`
		Token     string `xml:"NextContinuationToken"`
		Contents  []struct {
			Key      string
			Size     int64
			Modified time.Time `xml:"LastModified"`
			ETag     string
		} `xml:"Contents"`
		Prefixes []struct{ Prefix string } `xml:"CommonPrefixes"`
	}
	if err = xml.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&result); err != nil || result.XMLName.Local != "ListBucketResult" {
		return Page{}, errors.New("invalid S3 listing response")
	}
	page := Page{NextToken: result.Token}
	if result.Truncated && (page.NextToken == "" || page.NextToken == token) {
		return Page{}, errors.New("invalid S3 continuation token")
	}
	if !result.Truncated {
		page.NextToken = ""
	}
	for _, o := range result.Contents {
		if o.Size < 0 || !strings.HasPrefix(o.Key, prefix) {
			return Page{}, errors.New("invalid S3 listing key")
		}
		page.Objects = append(page.Objects, Object{Key: o.Key, Size: o.Size, ETag: o.ETag, Modified: o.Modified})
	}
	for _, p := range result.Prefixes {
		if !strings.HasPrefix(p.Prefix, prefix) {
			return Page{}, errors.New("invalid S3 listing prefix")
		}
		page.Prefixes = append(page.Prefixes, p.Prefix)
	}
	return page, nil
}

// Paginator advances only after a successful page; repeated tokens are rejected
// by List rather than spinning indefinitely on a malformed provider response.
type Paginator struct {
	client                   *Client
	prefix, delimiter, token string
	done                     bool
}

func NewPaginator(client *Client, prefix, delimiter string) *Paginator {
	return &Paginator{client: client, prefix: prefix, delimiter: delimiter}
}
func (p *Paginator) HasMorePages() bool { return !p.done }
func (p *Paginator) NextPage(ctx context.Context) (Page, error) {
	page, err := p.client.List(ctx, p.prefix, p.delimiter, p.token, 1000)
	if err != nil {
		return Page{}, err
	}
	p.token = page.NextToken
	p.done = p.token == ""
	return page, nil
}
