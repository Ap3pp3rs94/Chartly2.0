package store

import (
  "sort"
  "sync"
  "time"

  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/types"
)

type Store struct {
  mu sync.RWMutex
  topics map[string]*types.TopicProfile
  snapshots map[string]map[string]types.RawSnapshot
  runs []types.RunSummary
}

func New() *Store {
  return &Store{
    topics: map[string]*types.TopicProfile{},
    snapshots: map[string]map[string]types.RawSnapshot{},
    runs: []types.RunSummary{},
  }
}

func (s *Store) UpsertTopics(list []*types.TopicProfile) {
  s.mu.Lock()
  defer s.mu.Unlock()
  for _, t := range list {
    copy := *t
    s.topics[t.ID] = &copy
  }
}

func (s *Store) ListTopics() []*types.TopicProfile {
  s.mu.RLock(); defer s.mu.RUnlock()
  out := []*types.TopicProfile{}
  for _, t := range s.topics { out = append(out, t) }
  types.SortTopics(out)
  return out
}

func (s *Store) GetTopic(id string) *types.TopicProfile {
  s.mu.RLock(); defer s.mu.RUnlock()
  if t, ok := s.topics[id]; ok { return t }
  return nil
}

func (s *Store) CountActive() int {
  s.mu.RLock(); defer s.mu.RUnlock()
  c := 0
  for _, t := range s.topics { if t.Active { c++ } }
  return c
}

func (s *Store) SetActive(id string, active bool) {
  s.mu.Lock(); defer s.mu.Unlock()
  if t, ok := s.topics[id]; ok { t.Active = active }
}

func (s *Store) SaveSnapshot(topicID, sourceID string, snap types.RawSnapshot) {
  s.mu.Lock(); defer s.mu.Unlock()
  if _, ok := s.snapshots[topicID]; !ok { s.snapshots[topicID] = map[string]types.RawSnapshot{} }
  s.snapshots[topicID][sourceID] = snap
}

func (s *Store) GetSnapshots(topicID string) []types.RawSnapshot {
  s.mu.RLock(); defer s.mu.RUnlock()
  m := s.snapshots[topicID]
  out := []types.RawSnapshot{}
  for _, v := range m { out = append(out, v) }
  sort.Slice(out, func(i,j int) bool { return out[i].SourceID < out[j].SourceID })
  return out
}

func (s *Store) AddRun(r types.RunSummary) {
  s.mu.Lock(); defer s.mu.Unlock()
  s.runs = append(s.runs, r)
  if len(s.runs) > 200 { s.runs = s.runs[len(s.runs)-200:] }
}

func (s *Store) ListTopicSummaries() []types.TopicSummary {
  s.mu.RLock(); defer s.mu.RUnlock()
  out := []types.TopicSummary{}
  for _, t := range s.topics {
    snaps := s.snapshots[t.ID]
    lastFetch := time.Time{}
    if snaps != nil {
      for _, s := range snaps {
        if s.FetchedAt.After(lastFetch) { lastFetch = s.FetchedAt }
      }
    }
    out = append(out, types.TopicSummary{
      ID: t.ID,
      Name: t.Name,
      Active: t.Active,
      SourcesCount: len(t.Sources),
      LastFetchAt: lastFetch,
      LastProcessedAt: t.LastProcessedAt,
      Quality: t.Quality,
    })
  }
  sort.Slice(out, func(i,j int) bool { return out[i].ID < out[j].ID })
  return out
}


