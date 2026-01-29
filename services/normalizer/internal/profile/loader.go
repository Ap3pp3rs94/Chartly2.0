package profile

import (

	"encoding/json"

	"errors"

	"os"

	"path/filepath"

	"sort"

	"strings"


	"github.com/Ap3pp3rs94/Chartly2.0/services/normalizer/internal/engine"
)

type CleanserOptions struct {

	LowercaseKeys    bool `json:"lowercase_keys"`

	DropEmptyStrings bool `json:"drop_empty_strings"`
}

type QuarantinePolicy struct {

	Enabled bool `json:"enabled"`
}

type Profile struct {

	ID        string            `json:"id"`

	Version   string            `json:"version"`

	Match     map[string]string `json:"match,omitempty"` // connector_id, source_id, job_type supports "*"

	Pipeline  engine.Pipeline   `json:"pipeline"`

	Cleanser  CleanserOptions   `json:"cleanser"`

	Enrich    map[string]string `json:"enrich,omitempty"`

	SchemaID  string            `json:"schema_id,omitempty"`

	Quarantine QuarantinePolicy `json:"quarantine"`
}

type Loader struct {

	baseDir string
}

func NewLoader(baseDir string) *Loader {

	if strings.TrimSpace(baseDir) == "" {


		baseDir = "./profiles"

	}

	return &Loader{baseDir: baseDir}
}

func (l *Loader) LoadAll() ([]Profile, error) {

	ents, err := os.ReadDir(l.baseDir)

	if err != nil {


		if os.IsNotExist(err) {



			return nil, nil


		}


		return nil, err

	}


	files := make([]string, 0)

	for _, e := range ents {


		if e.IsDir() {



			continue


		}


		name := e.Name()


		if strings.HasSuffix(strings.ToLower(name), ".json") {



			files = append(files, filepath.Join(l.baseDir, name))


		}

	}

	sort.Strings(files)


	out := make([]Profile, 0, len(files))

	seen := make(map[string]struct{})


	for _, f := range files {


		b, err := os.ReadFile(f)


		if err != nil {



			return nil, err


		}


		var p Profile


		if err := json.Unmarshal(b, &p); err != nil {



			return nil, err


		}


		p.ID = strings.TrimSpace(p.ID)


		if p.ID == "" {



			return nil, errors.New("profile id empty: " + f)


		}


		if _, ok := seen[p.ID]; ok {



			return nil, errors.New("duplicate profile id: " + p.ID)


		}


		seen[p.ID] = struct{}{}


		if strings.TrimSpace(p.Version) == "" {



			p.Version = "v1"


		}


		// normalize match keys


		if p.Match != nil {



			nm := make(map[string]string, len(p.Match))



			for k, v := range p.Match {




				nm[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)



			}



			p.Match = nm


		}


		out = append(out, p)

	}


	return out, nil
}

func Select(meta map[string]string, profiles []Profile) (Profile, bool) {

	for _, p := range profiles {


		if match(p.Match, meta) {



			return p, true


		}

	}

	return Profile{}, false
}

func match(rule map[string]string, meta map[string]string) bool {

	if rule == nil || len(rule) == 0 {


		return true

	}

	for k, want := range rule {


		got := meta[strings.ToLower(strings.TrimSpace(k))]


		if want == "*" || want == "" {



			continue


		}


		if strings.TrimSpace(want) != strings.TrimSpace(got) {



			return false


		}

	}

	return true
}
