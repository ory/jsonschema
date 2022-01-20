package httploader

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bmizerany/assert"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/require"
)

var fooErr = errors.New("foo")

type rt struct{}

func (r rt) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, fooErr
}

var _ http.RoundTripper = new(rt)

func TestHTTPLoader(t *testing.T) {
	const expectedBody = "Hello, client"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(expectedBody))
	}))
	t.Cleanup(ts.Close)

	mr := func(t *testing.T, ctx context.Context) string {
		res, err := Load(context.WithValue(context.Background(), ContextKey, retryablehttp.NewClient()), ts.URL)
		require.NoError(t, err)
		defer res.Close()
		body, err := ioutil.ReadAll(res)
		require.NoError(t, err)
		return string(body)
	}

	assert.Equal(t, expectedBody, mr(t, context.Background()))

	hc := retryablehttp.NewClient()
	hc.RetryMax = 1
	hc.HTTPClient.Transport = new(rt)
	_, err := Load(context.WithValue(context.Background(), ContextKey, hc), ts.URL)
	require.ErrorIs(t, err, fooErr)

	_, err = Load(context.WithValue(context.Background(), ContextKey, new(struct{})), ts.URL)
	require.Error(t, err, fooErr)
	assert.Equal(t, "invalid context value for github.com/ory/jsonschema/v3/httploader.HTTPClient expected *retryablehttp.Client but got: *struct {}", err.Error())
}
