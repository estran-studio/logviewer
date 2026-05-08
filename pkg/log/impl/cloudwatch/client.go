// Package cloudwatch provides an implementation of logclient.LogBackend backed
// by AWS CloudWatch Logs and helper functions for building queries and parsing
// results.
package cloudwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// CWClient defines the interface for the AWS CloudWatch Logs client.
// This is used to allow for mocking in tests.
type CWClient interface {
	StartQuery(ctx context.Context, params *cloudwatchlogs.StartQueryInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error)
	GetQueryResults(ctx context.Context, params *cloudwatchlogs.GetQueryResultsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error)
	FilterLogEvents(ctx context.Context, params *cloudwatchlogs.FilterLogEventsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error)
}

// LogClient implements the client.LogBackend interface for AWS CloudWatch.
type LogClient struct {
	client CWClient
	logger *slog.Logger
}

// sanitizeQueryValue escapes single quotes in user provided values to safely embed them
// in CloudWatch Logs Insights query strings using single-quoted literals.
func sanitizeQueryValue(v string) string {
	// CloudWatch Logs Insights uses backslash as escape inside single quotes.
	// Replace single quote ' with \'
	return strings.ReplaceAll(v, "'", "\\'")
}

// isSafeFieldName ensures the field name only contains allowed runes (letters, digits, underscore, at-sign, and dot)
// to mitigate injection via crafted field names.
func isSafeFieldName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r == '@' || r == '_' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}

// Get executes a CloudWatch Logs query and returns the results.
//
//nolint:gocyclo // Complex search parameter handling and API orchestration
func (c *LogClient) Get(ctx context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	logGroupName, ok := search.Options.GetStringOk("logGroupName")
	if !ok {
		return nil, errors.New("logGroupName is required in options for CloudWatch Logs")
	}

	// Optional flag to disable Insights query (e.g., LocalStack) and fall back to FilterLogEvents API
	useInsights := true
	if v, ok := search.Options.GetBoolOk("useInsights"); ok {
		useInsights = v
	}

	// 1. Build the query string
	var queryParts []string
	// Always fetch the raw message and timestamp
	queryParts = append(queryParts, "fields @timestamp, @message")

	// Add filters from LogSearch.Fields with sanitization to avoid query injection.
	for key, value := range search.Fields {
		if !isSafeFieldName(key) {
			// Skip unsafe field names to avoid injection via the key itself.
			continue
		}
		sanitizedValue := sanitizeQueryValue(value)
		queryParts = append(queryParts, fmt.Sprintf(" | filter %s = '%s'", key, sanitizedValue))
	}

	// Add sorting and limits
	queryParts = append(queryParts, " | sort @timestamp desc")

	if search.PageToken.Set && search.PageToken.Value != "" {
		// The page token is the timestamp of the last event from the previous page.
		// We need to fetch events *before* this timestamp.
		// We also need to validate the token is a valid timestamp.
		if _, err := time.Parse(time.RFC3339Nano, search.PageToken.Value); err != nil {
			return nil, fmt.Errorf("invalid page token: expected a timestamp in RFC3339Nano format, got %s", search.PageToken.Value)
		}
		sanitizedToken := sanitizeQueryValue(search.PageToken.Value)
		queryParts = append(queryParts, fmt.Sprintf(" | filter @timestamp < timestamp('%s')", sanitizedToken))
	}

	if search.Size.Set {
		queryParts = append(queryParts, " | limit "+fmt.Sprintf("%d", search.Size.Value))
	}

	queryString := strings.Join(queryParts, "")

	// 2. Determine time range using search.Range (Last takes precedence over Gte/Lte)
	endTime := time.Now()
	startTime := endTime.Add(-1 * time.Hour) // default fallback

	// Helper to parse absolute timestamps (RFC3339 or Insights-like layout)
	parseAbs := func(v string) (time.Time, error) {
		if v == "" {
			return time.Time{}, errors.New("empty time string")
		}
		layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.000"}
		var lastErr error
		for _, l := range layouts {
			var ts time.Time
			var err error
			if ts, err = time.Parse(l, v); err == nil {
				return ts, nil
			}
			lastErr = err
		}
		return time.Time{}, lastErr
	}

	if search.Range.Last.Set && search.Range.Last.Value != "" {
		if d, err := time.ParseDuration(search.Range.Last.Value); err == nil {
			startTime = endTime.Add(-d)
		}
	}
	// Absolute range overrides default when provided
	if search.Range.Gte.Set && search.Range.Gte.Value != "" {
		if gte, err := parseAbs(search.Range.Gte.Value); err == nil {
			startTime = gte
		}
	}
	if search.Range.Lte.Set && search.Range.Lte.Value != "" {
		if lte, err := parseAbs(search.Range.Lte.Value); err == nil {
			endTime = lte
		}
	}
	// Ensure start <= end; if not, swap
	if startTime.After(endTime) {
		// If user supplied an inverted range, simply swap to preserve intent.
		startTime, endTime = endTime, startTime
	}

	// 3. Execute either Insights query or FilterLogEvents fallback
	if useInsights {
		startQueryOutput, err := c.client.StartQuery(ctx, &cloudwatchlogs.StartQueryInput{
			LogGroupName: aws.String(logGroupName),
			QueryString:  aws.String(queryString),
			StartTime:    aws.Int64(startTime.UnixMilli()),
			EndTime:      aws.Int64(endTime.UnixMilli()),
		})
		if err != nil {
			return nil, err
		}
		if startQueryOutput.QueryId == nil {
			return nil, errors.New("StartQuery did not return a QueryId")
		}
		return &LogSearchResult{client: c.client, queryID: *startQueryOutput.QueryId, search: search, logger: c.logger}, nil
	}

	// FilterLogEvents fallback
	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(logGroupName),
		StartTime:    aws.Int64(startTime.UnixMilli()),
		EndTime:      aws.Int64(endTime.UnixMilli()),
	}
	// Add filter pattern if simple equality filters are present (combine as AND)
	if len(search.Fields) > 0 {
		var parts []string
		for k, v := range search.Fields {
			if isSafeFieldName(k) {
				parts = append(parts, fmt.Sprintf("%s=\"%s\"", k, sanitizeQueryValue(v)))
			}
		}
		if len(parts) > 0 {
			p := strings.Join(parts, " ")
			input.FilterPattern = aws.String(p)
		}
	}
	// Page through results until size reached or no more
	entries := []client.LogEntry{}
	nextToken := aws.String("")
	for {
		if nextToken != nil && *nextToken != "" {
			input.NextToken = nextToken
		}
		out, err := c.client.FilterLogEvents(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, e := range out.Events {
			msg := ""
			if e.Message != nil {
				msg = *e.Message
			}
			ts := time.Unix(0, *e.Timestamp*int64(time.Millisecond))
			entries = append(entries, client.LogEntry{Timestamp: ts, Message: msg, Fields: ty.MI{}})
			if search.Size.Set && len(entries) >= search.Size.Value {
				break
			}
		}
		if search.Size.Set && len(entries) >= search.Size.Value {
			break
		}
		if out.NextToken == nil || (nextToken != nil && out.NextToken != nil && *out.NextToken == *nextToken) { // no forward progress
			break
		}
		nextToken = out.NextToken
		if nextToken == nil || *nextToken == "" {
			break
		}
	}
	// wrap entries in a simple LogSearchResult implementation
	return &staticCloudWatchResult{entries: entries, search: search}, nil
}

// staticCloudWatchResult is returned when using FilterLogEvents fallback (no async polling)
type staticCloudWatchResult struct {
	entries []client.LogEntry
	search  *client.LogSearch
}

func (r *staticCloudWatchResult) GetSearch() *client.LogSearch { return r.search }
func (r *staticCloudWatchResult) GetEntries(_ context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	return r.entries, nil, nil
}
func (r *staticCloudWatchResult) GetFields(_ context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return ty.UniSet[string]{}, nil, nil
}
func (r *staticCloudWatchResult) GetPaginationInfo() *client.PaginationInfo { return nil }
func (r *staticCloudWatchResult) Err() <-chan error                         { return nil }

// GetFieldValues retrieves distinct values for the specified fields.
func (c *LogClient) GetFieldValues(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
	// For CloudWatch, we need to run a search and extract field values from the results
	result, err := c.Get(ctx, search)
	if err != nil {
		return nil, err
	}
	return client.GetFieldValuesFromResult(ctx, result, fields)
}

// GetLogClient creates a new CloudWatch Logs client.
// It uses the 'region' and 'profile' from the options if provided.
func GetLogClient(options ty.MI) (client.LogBackend, error) {
	var cfgOptions []func(*config.LoadOptions) error

	// Region support (required for AWS SDK)
	if region, ok := options.GetStringOk("region"); ok {
		cfgOptions = append(cfgOptions, config.WithRegion(region))
	}

	// Shared profile support
	if profile, ok := options.GetStringOk("profile"); ok {
		cfgOptions = append(cfgOptions, config.WithSharedConfigProfile(profile))
	}

	// Optional custom endpoint (e.g. LocalStack) for integration tests
	// Key name: "endpoint" (lowercase) to be consistent with region/profile
	if endpoint, ok := options.GetStringOk("endpoint"); ok && endpoint != "" {
		resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, _ ...interface{}) (aws.Endpoint, error) { //nolint:staticcheck
			if strings.Contains(strings.ToLower(service), "logs") {
				return aws.Endpoint{URL: endpoint, PartitionID: "aws", SigningRegion: region}, nil //nolint:staticcheck
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{} //nolint:staticcheck
		})
		cfgOptions = append(cfgOptions, config.WithEndpointResolverWithOptions(resolver)) //nolint:staticcheck
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(), cfgOptions...)
	if err != nil {
		return nil, err
	}

	return &LogClient{client: cloudwatchlogs.NewFromConfig(cfg), logger: slog.Default()}, nil
}
