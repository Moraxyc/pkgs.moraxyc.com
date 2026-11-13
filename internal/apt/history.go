package apt

import (
	"errors"
	"path"
	"regexp"
	"strings"

	"pkgs.moraxyc.com/internal/config"
	"pkgs.moraxyc.com/internal/snapshot"
)

var historicalPath = regexp.MustCompile(`^deb/dists/stable/main/binary-[a-z0-9][a-z0-9-]*/by-hash/(SHA256/[0-9a-f]{64}|SHA512/[0-9a-f]{128})$`)

func retainedIndexes(history map[string]snapshot.HistoricalIndex, epoch int64) (map[string]snapshot.HistoricalIndex, error) {
	out := map[string]snapshot.HistoricalIndex{}
	for name, entry := range history {
		digest := Hash(entry.Data)
		if path.Base(path.Dir(name)) == "SHA512" {
			digest = hash512(entry.Data)
		}
		if !historicalPath.MatchString(name) || entry.ExpiresAt <= 0 || len(entry.Data) > 25<<20 || digest != path.Base(name) {
			return nil, errors.New("invalid historical index")
		}
		if entry.ExpiresAt > epoch {
			out[name] = entry
		}
	}
	return out, nil
}

// RetainIndexes retains by-hash bytes through Release validity plus 300 seconds
// of downstream caching, including across skipped deployments.
func RetainIndexes(c config.Config, records []snapshot.Record, history map[string]snapshot.HistoricalIndex, epoch int64) (map[string]snapshot.HistoricalIndex, error) {
	out, err := retainedIndexes(history, epoch)
	if err != nil {
		return nil, err
	}
	files, err := Indexes(c, records)
	if err != nil {
		return nil, err
	}
	expires := epoch + int64(c.Repository.ValidUntilDays)*86400 + 300
	for name, data := range files {
		if strings.Contains(name, "/by-hash/") {
			until := expires
			if out[name].ExpiresAt > until {
				until = out[name].ExpiresAt
			}
			out[name] = snapshot.HistoricalIndex{ExpiresAt: until, Data: data}
		}
	}
	if _, err = PublicationIndexes(c, records, out, epoch); err != nil {
		return nil, err
	}
	return out, nil
}

func PublicationIndexes(c config.Config, records []snapshot.Record, history map[string]snapshot.HistoricalIndex, epoch int64) (map[string][]byte, error) {
	files, err := Indexes(c, records)
	if err != nil {
		return nil, err
	}
	retained, err := retainedIndexes(history, epoch)
	if err != nil {
		return nil, err
	}
	for name, entry := range retained {
		files[name] = entry.Data
	}
	// Leave room for signatures, keyring, redirect rules and error page under
	// the Pages Free plan's 20,000-file limit.
	if len(files) > 19990 {
		return nil, errors.New("retained indexes exceed Pages file limit")
	}
	return files, nil
}
