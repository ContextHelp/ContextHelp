package s3

import (
	"bufio"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is an in-process S3 endpoint holding one bucket in memory:
// enough of PutObject, GetObject, HeadObject, DeleteObject and
// ListObjectsV2 (prefix, max-keys, continuation) for Store's tests.
type fakeS3 struct {
	t       *testing.T
	mu      sync.Mutex
	bucket  string
	objects map[string]fakeObject
}

type fakeObject struct {
	data     []byte
	modified time.Time
}

// newFakeS3 starts a fake S3 serving bucket and a Store using it, with
// store prefix prefix.
func newFakeS3(t *testing.T, bucket, prefix string) (*fakeS3, *Store) {
	t.Helper()
	f := &fakeS3{t: t, bucket: bucket, objects: map[string]fakeObject{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	s := newTestStore(t, srv)
	s.bucket, s.prefix = bucket, prefix
	return f, s
}

// put stores an object directly, bypassing Store.
func (f *fakeS3) put(key string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = fakeObject{data: data, modified: time.Now().UTC()}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != f.bucket {
		f.fail(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case key == "" && r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		f.list(w, r)
	case key == "":
		f.fail(w, http.StatusNotImplemented, "NotImplemented")
	case r.Method == http.MethodPut:
		data, err := readBody(r)
		if err != nil {
			f.fail(w, http.StatusBadRequest, "IncompleteBody")
			return
		}
		f.objects[key] = fakeObject{data: data, modified: time.Now().UTC()}
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		obj, ok := f.objects[key]
		if !ok {
			f.fail(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(obj.data)))
		w.Header().Set("Last-Modified", obj.modified.Format(http.TimeFormat))
		if r.Method == http.MethodGet {
			_, _ = w.Write(obj.data)
		}
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.fail(w, http.StatusNotImplemented, "NotImplemented")
	}
}

// readBody reads a PutObject body, decoding aws-chunked framing (a
// streamed body with a trailing checksum) when the SDK sends one.
func readBody(r *http.Request) ([]byte, error) {
	if !strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") &&
		!strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
		return io.ReadAll(r.Body)
	}
	var out []byte
	br := bufio.NewReader(r.Body)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeHex, _, _ := strings.Cut(strings.TrimSpace(line), ";")
		n, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return out, nil // trailers follow; ignored
		}
		chunk := make([]byte, n)
		if _, err := io.ReadFull(br, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if _, err := br.ReadString('\n'); err != nil {
			return nil, err
		}
	}
}

type listResult struct {
	XMLName               xml.Name      `xml:"ListBucketResult"`
	Name                  string        `xml:"Name"`
	Prefix                string        `xml:"Prefix"`
	KeyCount              int           `xml:"KeyCount"`
	MaxKeys               int           `xml:"MaxKeys"`
	IsTruncated           bool          `xml:"IsTruncated"`
	NextContinuationToken string        `xml:"NextContinuationToken,omitempty"`
	Contents              []listContent `xml:"Contents"`
}

type listContent struct {
	Key          string `xml:"Key"`
	Size         int    `xml:"Size"`
	LastModified string `xml:"LastModified"`
}

// list answers ListObjectsV2: keys with the prefix in byte order, at
// most max-keys (default 1000) a page, the continuation token being
// the last key returned.
func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	prefix, after := q.Get("prefix"), q.Get("continuation-token")
	maxKeys := 1000
	if v, err := strconv.Atoi(q.Get("max-keys")); err == nil && v > 0 {
		maxKeys = v
	}
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) && key > after {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	res := listResult{Name: f.bucket, Prefix: prefix, MaxKeys: maxKeys}
	if len(keys) > maxKeys {
		keys = keys[:maxKeys]
		res.IsTruncated = true
		res.NextContinuationToken = keys[len(keys)-1]
	}
	for _, key := range keys {
		obj := f.objects[key]
		res.Contents = append(res.Contents, listContent{
			Key: key, Size: len(obj.data), LastModified: obj.modified.Format("2006-01-02T15:04:05.000Z"),
		})
	}
	res.KeyCount = len(res.Contents)
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, xml.Header)
	if err := xml.NewEncoder(w).Encode(res); err != nil {
		f.t.Errorf("fake s3: encode listing: %v", err)
	}
}

func (f *fakeS3) fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, xml.Header+"<Error><Code>"+code+"</Code><Message>"+code+"</Message></Error>")
}
