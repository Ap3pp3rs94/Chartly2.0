package types

import (
  "sort"
  "time"
)

type Source struct {
  ID string `yaml:"id" json:"id"`
  Kind string `yaml:"kind" json:"kind"`
  URL string `yaml:"url" json:"url"`
  Auth string `yaml:"auth" json:"auth"`
  Tags []string `yaml:"tags" json:"tags"`
}

type TopicProfile struct {
  ID string `yaml:"id" json:"id"`
  Name string `yaml:"name" json:"name"`
  Version string `yaml:"version" json:"version"`
  Description string `yaml:"description" json:"description"`
  Active bool `yaml:"active" json:"active"`
  Sources []Source `yaml:"sources" json:"sources"`
  LastProcessedAt time.Time `json:"last_processed_at"`
  Quality Quality `json:"quality"`
}

type Quality struct {
  Health string `json:"health"`
  MissingVars []string `json:"missing_vars"`
  LastError string `json:"last_error"`
}

type RawSnapshot struct {
  TopicID string `json:"topic_id"`
  SourceID string `json:"source_id"`
  FetchedAt time.Time `json:"fetched_at"`
  BodyHash string `json:"body_hash"`
  BodyBytesTruncated bool `json:"body_bytes_truncated"`
  ExtractedPreview any `json:"extracted_preview"`
  Error string `json:"error,omitempty"`
}

type TopicSummary struct {
  ID string `json:"id"`
  Name string `json:"name"`
  Active bool `json:"active"`
  SourcesCount int `json:"sources_count"`
  LastFetchAt time.Time `json:"last_fetch_at"`
  LastProcessedAt time.Time `json:"last_processed_at"`
  Quality Quality `json:"quality"`
}

type RunSummary struct {
  TopicID string `json:"topic_id"`
  StartedAt time.Time `json:"started_at"`
  DurationMs int64 `json:"duration_ms"`
  RecordsIn int `json:"records_in"`
  RecordsOut int `json:"records_out"`
  Status string `json:"status"`
  Error string `json:"error"`
}

type TopicOutput struct {
  TopicID string `json:"topic_id"`
  AsOf string `json:"as_of"`
  Signals struct {
    Price struct { USD float64 `json:"usd"`; Change24hPct float64 `json:"change_24h_pct"`; Volume24h float64 `json:"volume_24h"` } `json:"price"`
    Sentiment struct { Score float64 `json:"score"`; Label string `json:"label"` } `json:"sentiment"`
    News struct { Count24h int `json:"count_24h"`; TopHeadlines []struct{ Title string `json:"title"`; URL string `json:"url"` } `json:"top_headlines"` } `json:"news"`
    Chatter struct { HnMentions24h int `json:"hn_mentions_24h"` } `json:"chatter"`
    Dev struct { GithubCommits7d int `json:"github_commits_7d"` } `json:"dev"`
    Regulatory struct { Mentions24h int `json:"mentions_24h"` } `json:"regulatory"`
  } `json:"signals"`
  Insights []struct{ Type string `json:"type"`; Message string `json:"message"`; Confidence string `json:"confidence"` } `json:"insights"`
  OutputHash string `json:"output_hash"`
}

func SortSources(src []Source) []Source {
  sort.Slice(src, func(i,j int) bool { return src[i].ID < src[j].ID })
  return src
}

func SortTopics(t []*TopicProfile) {
  sort.Slice(t, func(i,j int) bool { return t[i].ID < t[j].ID })
}

