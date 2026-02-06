package fetcher

import (
  "context"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "encoding/xml"
  "io"
  "net/http"
  "strings"
  "sync"
  "time"

  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/store"
  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/types"
)

type Options struct {
  PerSourceTimeout time.Duration
  SampleLimit int
  MaxRawBytes int
  PerTopicMaxSources int
}

type rssFeed struct {
  Channel struct {
    Items []struct {
      Title string `xml:"title"`
      Link string `xml:"link"`
      PubDate string `xml:"pubDate"`
    } `xml:"item"`
  } `xml:"channel"`
}

func FetchAll(ctx context.Context, st *store.Store, topics []*types.TopicProfile, opt Options) {
  var wg sync.WaitGroup
  for _, t := range topics {
    topic := t
    wg.Add(1)
    go func() {
      defer wg.Done()
      FetchTopic(ctx, st, topic, opt)
    }()
  }
  wg.Wait()
}

func FetchTopic(ctx context.Context, st *store.Store, topic *types.TopicProfile, opt Options) {
  sources := topic.Sources
  if opt.PerTopicMaxSources > 0 && len(sources) > opt.PerTopicMaxSources {
    sources = sources[:opt.PerTopicMaxSources]
  }
  for _, src := range sources {
    snap := fetchSource(ctx, topic.ID, src, opt)
    st.SaveSnapshot(topic.ID, src.ID, snap)
  }
}

func fetchSource(ctx context.Context, topicID string, src types.Source, opt Options) types.RawSnapshot {
  c, cancel := context.WithTimeout(ctx, opt.PerSourceTimeout)
  defer cancel()

  req, _ := http.NewRequestWithContext(c, http.MethodGet, src.URL, nil)
  req.Header.Set("User-Agent", "Chartly-Topics/1.0")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    return types.RawSnapshot{TopicID: topicID, SourceID: src.ID, FetchedAt: time.Now().UTC(), Error: err.Error()}
  }
  defer resp.Body.Close()

  limited := io.LimitReader(resp.Body, int64(opt.MaxRawBytes))
  body, _ := io.ReadAll(limited)
  truncated := false
  if resp.ContentLength > int64(opt.MaxRawBytes) { truncated = true }

  hash := sha256.Sum256(body)
  preview := extractPreview(src.Kind, body)

  return types.RawSnapshot{
    TopicID: topicID,
    SourceID: src.ID,
    FetchedAt: time.Now().UTC(),
    BodyHash: hex.EncodeToString(hash[:]),
    BodyBytesTruncated: truncated,
    ExtractedPreview: preview,
  }
}

func extractPreview(kind string, body []byte) any {
  switch strings.ToLower(kind) {
  case "rss":
    var feed rssFeed
    if err := xml.Unmarshal(body, &feed); err != nil { return nil }
    items := feed.Channel.Items
    if len(items) > 20 { items = items[:20] }
    out := []map[string]string{}
    for _, it := range items {
      out = append(out, map[string]string{"title": it.Title, "url": it.Link})
    }
    return out
  default:
    var v any
    if err := json.Unmarshal(body, &v); err != nil { return string(body) }
    return v
  }
}


