package cloudwatch

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

// mock client for sanitization tests
type mockCWClientSanitize struct {
	startCaptured *cloudwatchlogs.StartQueryInput
}

func (m *mockCWClientSanitize) StartQuery(_ context.Context, params *cloudwatchlogs.StartQueryInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error) {
	m.startCaptured = params
	return &cloudwatchlogs.StartQueryOutput{QueryId: aws.String("qid")}, nil
}

func (m *mockCWClientSanitize) GetQueryResults(_ context.Context, _ *cloudwatchlogs.GetQueryResultsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
	return &cloudwatchlogs.GetQueryResultsOutput{}, nil
}
func (m *mockCWClientSanitize) FilterLogEvents(_ context.Context, _ *cloudwatchlogs.FilterLogEventsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	return &cloudwatchlogs.FilterLogEventsOutput{}, nil
}

func TestSanitizeQueryValue(t *testing.T) {
	in := "value'with"
	out := sanitizeQueryValue(in)
	assert.Equal(t, "value\\'with", out)
}

func TestIsSafeFieldName(t *testing.T) {
	assert.True(t, isSafeFieldName("level"))
	assert.True(t, isSafeFieldName("@timestamp"))
	assert.False(t, isSafeFieldName("level;drop"))
	assert.False(t, isSafeFieldName(""))
}

func TestQueryBuildingEscapesValues(t *testing.T) {
	mockClient := &mockCWClientSanitize{}
	c := &LogClient{client: mockClient}
	s := &client.LogSearch{Fields: ty.MS{"level": "error'critical"}, Options: ty.MI{"logGroupName": "lg"}}
	_, err := c.Get(context.Background(), s)
	assert.NoError(t, err)
	qs := *mockClient.startCaptured.QueryString
	assert.Contains(t, qs, "filter level = 'error\\'critical'")
}
