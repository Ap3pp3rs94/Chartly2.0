package discovery

import "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/types"

func Discover(topicID string) []types.Source {
  switch topicID {
  case "bitcoin":
    return types.SortSources([]types.Source{
      {ID:"alternative_fng", Kind:"http_json", URL:"https://api.alternative.me/fng/?limit=1&format=json", Auth:"none", Tags: []string{"sentiment"}},
      {ID:"coindesk_rss", Kind:"rss", URL:"https://www.coindesk.com/arc/outboundfeeds/rss/", Auth:"none", Tags: []string{"news"}},
      {ID:"coingecko_price", Kind:"http_json", URL:"https://api.coingecko.com/api/v3/coins/bitcoin?localization=false&tickers=false&market_data=true&community_data=false&developer_data=true&sparkline=false", Auth:"none", Tags: []string{"price","market","meta"}},
      {ID:"github_commits_bitcoin", Kind:"http_json", URL:"https://api.github.com/repos/bitcoin/bitcoin/commits?per_page=30", Auth:"none", Tags: []string{"dev"}},
      {ID:"hn_search", Kind:"http_json", URL:"https://hn.algolia.com/api/v1/search?query=bitcoin&tags=story", Auth:"none", Tags: []string{"chatter"}},
    })
  default:
    return []types.Source{}
  }
}


