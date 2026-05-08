package logclient

import (
	"context"
	"testing"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/impl/splunk/restapi"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/h2non/gock"
	"github.com/stretchr/testify/assert"
)

func TestSplunkLogClient(t *testing.T) {

	gock.New("http://splunk.com:8080").
		Post("/search/jobs").
		MatchType("application/x-www-form-urlencoded").
		Reply(200).
		JSON(ty.MI{"Sid": "mycid"})

	gock.New("http://splunk.com:8080").
		Get("/search/jobs/mycid").
		Reply(200).
		JSON(ty.MI{
			"entry": []ty.MI{
				{
					"content": ty.MI{
						"isDone": true,
					},
				},
			},
		})

	gock.New("http://splunk.com:8080").
		Get("/search/jobs/mycid/events").
		Reply(200).
		JSON(ty.MI{
			"results": []ty.MS{
				{
					"_raw":             "mylogentry",
					"_subsecond":       ".681",
					"_time":            "2024-06-21T08:56:05.681-07:00",
					"application_name": "wq.services.pet",
					"cat":              "BusinessExceptionHandler",
					"handler":          "CreatePet",
				},
				{
					"_raw":             "mylogentry",
					"_subsecond":       ".681",
					"_time":            "2024-06-21T08:56:05.681-07:00",
					"application_name": "wq.services.pet",
					"cat":              "BusinessExceptionHandler",
					"handler":          "DeletePet",
				},
			},
		})

	logClient, err := GetClient(SplunkLogSearchClientOptions{
		URL: "http://splunk.com:8080",
	})

	if err != nil {
		t.Error(err)
	}

	logSearch := client.LogSearch{
		Fields:  ty.MS{},
		Options: ty.MI{},
	}
	logSearch.Range.Gte.S("24h@h")
	logSearch.Range.Lte.S("now")

	logSearch.Fields["application_name"] = "wq.services.pet"
	logSearch.Options["index"] = "prd3392"

	result, err := logClient.Get(context.Background(), &logSearch)
	if err != nil {
		t.Error(err)
	}

	fields, _, err := result.GetFields(context.Background())
	if err != nil {
		t.Error(err)
	}

	logEntry, _, err := result.GetEntries(context.Background())
	if err != nil {
		t.Error(err)
	}

	logTimestamp, _ := time.Parse(time.RFC3339, "2024-06-21T08:56:05.681-07:00")

	assert.Equal(t, []string{"CreatePet", "DeletePet"}, fields["handler"])
	assert.Equal(t, "mylogentry", logEntry[0].Message)
	assert.Equal(t, "CreatePet", logEntry[0].Fields.GetString("handler"))
	assert.Equal(t, logTimestamp, logEntry[0].Timestamp)

}

func TestSplunkLogSearchResult_GetPaginationInfo(t *testing.T) {
	t.Run("no size set, no pagination", func(t *testing.T) {
		search := &client.LogSearch{}
		result := SplunkLogSearchResult{search: search}
		assert.Nil(t, result.GetPaginationInfo())
	})

	t.Run("results less than size, no more pages", func(t *testing.T) {
		search := &client.LogSearch{Size: ty.Opt[int]{Value: 10, Set: true}}
		result := SplunkLogSearchResult{
			search: search,
			results: []restapi.SearchResultsResponse{
				{Results: make([]ty.MI, 5)},
			},
		}
		assert.Nil(t, result.GetPaginationInfo())
	})

	t.Run("results equal size, more pages", func(t *testing.T) {
		search := &client.LogSearch{Size: ty.Opt[int]{Value: 10, Set: true}}
		result := SplunkLogSearchResult{
			search: search,
			results: []restapi.SearchResultsResponse{
				{Results: make([]ty.MI, 10)},
			},
		}
		paginationInfo := result.GetPaginationInfo()
		assert.NotNil(t, paginationInfo)
		assert.True(t, paginationInfo.HasMore)
		assert.Equal(t, "10", paginationInfo.NextPageToken)
	})

	t.Run("with existing page token", func(t *testing.T) {
		search := &client.LogSearch{
			Size:      ty.Opt[int]{Value: 10, Set: true},
			PageToken: ty.Opt[string]{Value: "10", Set: true},
		}
		result := SplunkLogSearchResult{
			search: search,
			results: []restapi.SearchResultsResponse{
				{Results: make([]ty.MI, 10)},
			},
			CurrentOffset: 10,
		}
		paginationInfo := result.GetPaginationInfo()
		assert.NotNil(t, paginationInfo)
		assert.True(t, paginationInfo.HasMore)
		assert.Equal(t, "20", paginationInfo.NextPageToken)
	})

	t.Run("invalid page token", func(t *testing.T) {
		search := &client.LogSearch{
			Size:      ty.Opt[int]{Value: 10, Set: true},
			PageToken: ty.Opt[string]{Value: "invalid", Set: true},
		}
		result := SplunkLogSearchResult{
			search: search,
			results: []restapi.SearchResultsResponse{
				{Results: make([]ty.MI, 10)},
			},
		}
		paginationInfo := result.GetPaginationInfo()
		assert.NotNil(t, paginationInfo)
		assert.True(t, paginationInfo.HasMore)
		assert.Equal(t, "10", paginationInfo.NextPageToken)
	})
}

func TestSplunkLogSearchClient_Get_Follow(t *testing.T) {
	defer gock.Off()

	gock.New("http://splunk.com:8080").
		Post("/search/jobs").
		MatchType("application/x-www-form-urlencoded").
		BodyString("search_mode=realtime").
		Reply(200).
		JSON(ty.MI{"sid": "my-follow-sid"})

	logClient, err := GetClient(SplunkLogSearchClientOptions{
		URL: "http://splunk.com:8080",
	})
	assert.NoError(t, err)

	logSearch := client.LogSearch{
		Follow: true,
	}

	result, err := logClient.Get(context.Background(), &logSearch)
	assert.NoError(t, err)

	splunkResult, ok := result.(SplunkLogSearchResult)
	assert.True(t, ok)

	assert.True(t, splunkResult.isFollow)
	assert.Equal(t, "my-follow-sid", splunkResult.sid)

	assert.True(t, gock.IsDone())
}
