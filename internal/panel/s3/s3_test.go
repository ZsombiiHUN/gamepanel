package s3

import (
	"net/url"
	"testing"
	"time"
)

// The presigned URL example from the AWS documentation ("Authenticating
// Requests: Using Query Parameters"), which fixes every input.
func TestPresignMatchesAWSExample(t *testing.T) {
	c, err := New(Config{Endpoint: "https://s3.amazonaws.com", Region: "us-east-1", Bucket: "examplebucket",
		AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"})
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) }
	u, _ := url.Parse(c.Presign("GET", "test.txt", 86400*time.Second, nil))
	if u.Host != "examplebucket.s3.amazonaws.com" || u.Path != "/test.txt" {
		t.Fatalf("url %s", u)
	}
	if got := u.Query().Get("X-Amz-Signature"); got != "aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404" {
		t.Fatalf("signature %s", got)
	}
}

func TestPathStyleAndKeys(t *testing.T) {
	c, _ := New(Config{Endpoint: "http://minio:9000/", Bucket: "b", AccessKey: "a", SecretKey: "s", PathStyle: true, Prefix: "gp/"})
	u, _ := url.Parse(c.Presign("PUT", c.Key("srv", "bk"), time.Minute, nil))
	if u.Host != "minio:9000" || u.Path != "/b/gp/srv/bk.tar.gz" {
		t.Fatalf("url %s", u)
	}
}

func TestPartSize(t *testing.T) {
	for _, total := range []int64{0, 1, 64 << 20, 700 << 30, 5 << 40} {
		size := PartSize(total)
		if size < 64<<20 || (total+size-1)/size > 10000 {
			t.Errorf("total %d: part size %d", total, size)
		}
	}
}

func TestEscape(t *testing.T) {
	if got := escape("a b/c+d~", true); got != "a%20b/c%2Bd~" {
		t.Fatal(got)
	}
	if got := escape("a/b", false); got != "a%2Fb" {
		t.Fatal(got)
	}
}
