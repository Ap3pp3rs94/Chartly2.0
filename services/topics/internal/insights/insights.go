package insights

import (
  "bytes"
  "context"
  "encoding/json"
  "net/http"

  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/types"
)

func PostResult(ctx context.Context, controlPlaneURL, resultsPath string, out types.TopicOutput) {
  runID := "topic:" + out.TopicID + ":" + out.OutputHash
  body := map[string]any{
    "drone_id": "topics-codex",
    "profile_id": "topic:" + out.TopicID,
    "run_id": runID,
    "data": []any{out},
  }
  b, _ := json.Marshal(body)
  req, _ := http.NewRequestWithContext(ctx, http.MethodPost, controlPlaneURL+resultsPath, bytes.NewReader(b))
  req.Header.Set("Content-Type", "application/json")
  _, _ = http.DefaultClient.Do(req)
}


