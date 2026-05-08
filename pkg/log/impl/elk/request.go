// Package elk contains helpers for building OpenSearch/Kibana-compatible
// search request fragments and parsing results.
package elk

import (
	"errors"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
)

// GetDateRange calculates the date range (gte, lte) for a search.
func GetDateRange(search *client.LogSearch) (string, string, error) {
	var gte, lte string

	var fromDate time.Time
	var err error

	if search.Size.Value == 0 {
		search.Size.S(100)
	}

	if search.Range.Lte.Value != "" {
		fromDate, err = time.Parse(ty.Format, search.Range.Lte.Value)
		if err != nil {
			return "", "", errors.New("can't parse lte date")
		}
		lte = search.Range.Lte.Value
	} else {
		fromDate = time.Now()
		lte = fromDate.Format(ty.Format)
	}

	if search.Range.Gte.Value != "" {
		gte = search.Range.Gte.Value
	} else {
		if !search.Range.Last.Valid {
			return "", "", errors.New("if not Range.Gte provided must provied Range.Last")
		}
		if duration, err := time.ParseDuration(search.Range.Last.Value); err == nil {
			gte = fromDate.Add(-duration).Format(ty.Format)
		} else {
			return "", "", errors.New("can't parse duration for last : " + search.Range.Last.Value)
		}
	}

	search.Range.Lte.S(lte)
	search.Range.Gte.S(gte)

	return gte, lte, nil
}

// GetDateRangeConditon builds the range condition for a query.
func GetDateRangeConditon(gte, lte string) ty.MI {

	return ty.MI{
		"range": ty.MI{
			"@timestamp": ty.MI{
				"format": "strict_date_optional_time",
				"gte":    gte,
				"lte":    lte,
			},
		},
	}
}
