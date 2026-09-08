package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// populationPriorStrength is the number of population observations that
// regularize a creator's early library. A creator's evidence gains weight as
// its sample count grows, without making a first take override the population.
const populationPriorStrength = 10

// DurationPrior predicts how quickly a speaker is likely to speak translated text.
// PopulationSamples and CreatorSamples report the independent, deduplicated row
// counts behind the prediction. CreatorWeight identifies how much the creator's
// observed rate contributes to CharsPerSecond.
type DurationPrior struct {
	CharsPerSecond    float64
	PopulationSamples uint64
	CreatorSamples    uint64
	CreatorWeight     float64
}

type durationPriorStats struct {
	PopulationSamples        uint64  `json:"population_samples"`
	PopulationCharsPerSecond float64 `json:"population_chars_per_sec"`
	CreatorSamples           uint64  `json:"creator_samples"`
	CreatorCharsPerSecond    float64 `json:"creator_chars_per_sec"`
}

const selectDurationPrior = "SELECT toUInt64(count()) AS population_samples, coalesce(avg(chars_per_sec), 0.) AS population_chars_per_sec, toUInt64(countIf(owner_id = {owner_id:String})) AS creator_samples, coalesce(avgIf(chars_per_sec, owner_id = {owner_id:String}), 0.) AS creator_chars_per_sec FROM take_rates WHERE language = {language:String} AND speaker = {speaker:String} FORMAT JSONEachRow"

// DurationPrior returns a learned character-per-second estimate for one creator,
// language, and speaker. A new creator receives the population rate. As the
// creator library grows, its rate receives creatorSamples/(creatorSamples+10) of
// the estimate. The query reads take_rates, whose FINAL read makes both counts
// honest after a retried durable write.
func (c *Client) DurationPrior(ctx context.Context, ownerID, language, speaker string) (DurationPrior, error) {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "owner_id", value: ownerID},
		{name: "language", value: language},
		{name: "speaker", value: speaker},
	} {
		if strings.TrimSpace(field.value) == "" {
			return DurationPrior{}, errors.New("ledger duration prior " + field.name + " is empty")
		}
	}

	stats, err := c.durationPriorStats(ctx, ownerID, language, speaker)
	if err != nil {
		return DurationPrior{}, err
	}
	if stats.PopulationSamples == 0 {
		return DurationPrior{}, fmt.Errorf("ledger duration prior has no population samples for language %q and speaker %q", language, speaker)
	}

	prior := DurationPrior{
		PopulationSamples: stats.PopulationSamples,
		CreatorSamples:    stats.CreatorSamples,
	}
	if stats.CreatorSamples == 0 {
		prior.CharsPerSecond = stats.PopulationCharsPerSecond
		return prior, nil
	}
	prior.CreatorWeight = float64(stats.CreatorSamples) / (float64(stats.CreatorSamples) + populationPriorStrength)
	prior.CharsPerSecond = prior.CreatorWeight*stats.CreatorCharsPerSecond + (1-prior.CreatorWeight)*stats.PopulationCharsPerSecond
	return prior, nil
}

func (c *Client) durationPriorStats(ctx context.Context, ownerID, language, speaker string) (durationPriorStats, error) {
	if c == nil || c.endpoint == nil || c.http == nil {
		return durationPriorStats{}, errors.New("ledger client is nil")
	}
	requestURL := *c.endpoint
	query := requestURL.Query()
	query.Set("database", c.database)
	query.Set("query", selectDurationPrior)
	query.Set("param_owner_id", ownerID)
	query.Set("param_language", language)
	query.Set("param_speaker", speaker)
	requestURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), bytes.NewReader(nil))
	if err != nil {
		return durationPriorStats{}, fmt.Errorf("create ClickHouse duration prior query: %w", err)
	}
	req.SetBasicAuth(c.user, c.password)
	resp, err := c.http.Do(req)
	if err != nil {
		return durationPriorStats{}, fmt.Errorf("send ClickHouse duration prior query: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return durationPriorStats{}, fmt.Errorf("ClickHouse returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	var stats durationPriorStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); errors.Is(err, io.EOF) {
		return durationPriorStats{}, errors.New("ClickHouse returned no duration prior row")
	} else if err != nil {
		return durationPriorStats{}, fmt.Errorf("decode ClickHouse duration prior query: %w", err)
	}
	return stats, nil
}
