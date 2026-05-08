package restapi

import (
	"testing"

	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

func TestBuildSearchJobData_DefaultTimes_NoCustomSearch(t *testing.T) {
	body := buildSearchJobData("index=main", "", "", false, nil)

	// ensure earliest/latest were defaulted
	assert.Equal(t, "-24h@h", body["earliest_time"])
	assert.Equal(t, "now", body["latest_time"])

	// ensure search param is present and contains the prefixed "search "
	assert.Equal(t, "search index=main", body["search"])

	// ensure custom.search is not present
	_, ok := body["custom.search"]
	assert.False(t, ok)

	// ensure function does not mutate caller's map when nil passed
	m := ty.MS{"foo": "bar"}
	out := buildSearchJobData("index=main", "2020-01-01", "2020-01-02", false, m)
	assert.Equal(t, "2020-01-02", out["latest_time"])
	assert.Equal(t, "2020-01-01", out["earliest_time"])
}

func TestBuildSearchJobData_RealTime(t *testing.T) {
	// Test case 1: No time range provided
	body1 := buildSearchJobData("index=main", "", "", true, nil)
	assert.Equal(t, "realtime", body1["search_mode"])
	assert.Equal(t, "rt-5m", body1["earliest_time"])
	assert.Equal(t, "rt", body1["latest_time"])
	assert.Equal(t, "search index=main", body1["search"])

	// Test case 2: Time range provided
	body2 := buildSearchJobData("index=main", "-1h", "", true, nil)
	assert.Equal(t, "realtime", body2["search_mode"])
	assert.Equal(t, "rt-1h", body2["earliest_time"])
	assert.Equal(t, "rt", body2["latest_time"])

	// Test case 3: Time range with rt prefix already present
	body3 := buildSearchJobData("index=main", "rt-2h", "rt-1h", true, nil)
	assert.Equal(t, "realtime", body3["search_mode"])
	assert.Equal(t, "rt-2h", body3["earliest_time"])
	assert.Equal(t, "rt-1h", body3["latest_time"])
}
