package normalize

import (
  "bytes"
  "context"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "fmt"
  "net/http"
  "sort"
  "time"

  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/store"
  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/types"
)

type Options struct {
  ExecutorURL string
  ExecutorPath string
  MaxTokens int
}

func ProcessBatch(ctx context.Context, opt Options, st *store.Store, topics []*types.TopicProfile) ([]types.TopicOutput, error) {
  // batch up to 10
  sort.Slice(topics, func(i,j int) bool { return topics[i].ID < topics[j].ID })
  if len(topics) > 10 { topics = topics[:10] }

  payload := buildPrompt(topics, st)
  reqBody := map[string]any{
    "runner_id": "topics-codex",
    "profile_id": "topics-batch",
    "temperature": 0,
    "max_tokens": opt.MaxTokens,
    "timeout_ms": 60000,
    "prompt": payload,
  }

  b, _ := json.Marshal(reqBody)
  req, _ := http.NewRequestWithContext(ctx, http.MethodPost, opt.ExecutorURL+opt.ExecutorPath, bytes.NewReader(b))
  req.Header.Set("Content-Type", "application/json")

  resp, err := http.DefaultClient.Do(req)
  if err != nil { return nil, err }
  defer resp.Body.Close()

  var out struct{ Ok bool `json:"ok"`; Output string `json:"output"` }
  if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return nil, err }
  if !out.Ok { return nil, fmt.Errorf("executor not ok") }

  return validateOutput(out.Output, topics)
}

func buildPrompt(topics []*types.TopicProfile, st *store.Store) string {
  type promptTopic struct {
    ID string `json:"topic_id"`
    Name string `json:"name"`
    Sources []any `json:"sources"`
  }
  list := []promptTopic{}
  for _, t := range topics {
    snaps := st.GetSnapshots(t.ID)
    sources := []any{}
    for _, s := range snaps {
      sources = append(sources, map[string]any{
        "source_id": s.SourceID,
        "preview": s.ExtractedPreview,
      })
    }
    list = append(list, promptTopic{ID: t.ID, Name: t.Name, Sources: sources})
  }

  req := map[string]any{
    "instruction": "OUTPUT MUST BE STRICT JSON ONLY. NO MARKDOWN. Use the provided schema exactly.",
    "schema": map[string]any{
      "ok": true,
      "topics": []any{
        map[string]any{
          "topic_id": "bitcoin",
          "as_of": time.Now().UTC().Format(time.RFC3339),
          "signals": map[string]any{
            "price": map[string]any{"usd": 0, "change_24h_pct": 0, "volume_24h": 0},
            "sentiment": map[string]any{"score": 0, "label": ""},
            "news": map[string]any{"count_24h": 0, "top_headlines": []any{map[string]any{"title":"","url":""}}},
            "chatter": map[string]any{"hn_mentions_24h": 0},
            "dev": map[string]any{"github_commits_7d": 0},
            "regulatory": map[string]any{"mentions_24h": 0},
          },
          "insights": []any{map[string]any{"type":"note","message":"","confidence":"low"}},
        },
      },
    },
    "topics": list,
  }
  b, _ := json.Marshal(req)
  return string(b)
}

func validateOutput(output string, topics []*types.TopicProfile) ([]types.TopicOutput, error) {
  var root struct {
    Ok bool `json:"ok"`
    Topics []types.TopicOutput `json:"topics"`
  }
  if err := json.Unmarshal([]byte(output), &root); err != nil { return nil, err }
  if !root.Ok { return nil, fmt.Errorf("not ok") }

  allowed := map[string]bool{}
  for _, t := range topics { allowed[t.ID] = true }

  out := []types.TopicOutput{}
  for _, t := range root.Topics {
    if !allowed[t.TopicID] { continue }
    if len(t.Signals.News.TopHeadlines) > 10 {
      t.Signals.News.TopHeadlines = t.Signals.News.TopHeadlines[:10]
    }
    if len(t.Insights) > 20 {
      t.Insights = t.Insights[:20]
    }
    // compute output hash
    b := canonicalJSON(t)
    h := sha256.Sum256(b)
    t.OutputHash = hex.EncodeToString(h[:])
    out = append(out, t)
  }
  return out, nil
}

func canonicalJSON(v any) []byte {
  b, _ := json.Marshal(v)
  return b
}


