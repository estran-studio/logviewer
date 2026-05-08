//nolint:revive // intentional package name for testing
package http

import (
	"testing"

	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/h2non/gock"
	"github.com/stretchr/testify/assert"
)

func TestHttpClient_Get_SendsHeaders(t *testing.T) {
	defer gock.Off()
	gock.DisableNetworking()

	url := "http://example.com"

	gock.New(url).
		Get("/test").
		MatchHeader("X-Custom-Header", "custom-value").
		MatchHeader("Authorization", "Basic dXNlcjpwYXNz").
		Reply(200).
		JSON(map[string]string{"status": "ok"})

	client := GetClient(url, nil)

	headers := ty.MS{
		"X-Custom-Header": "custom-value",
		"Authorization":   "Basic dXNlcjpwYXNz",
	}

	var response map[string]string
	err := client.Get("/test", nil, headers, nil, &response, nil)

	assert.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
	assert.True(t, gock.IsDone())
}
