// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package gossip

import (
	"bytes"
	"sort"
	"time"

	"github.com/cockroachdb/cockroach/pkg/config"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/util/timeutil"
)

// SystemConfigDeltaFilter keeps track of SystemConfig values so that unmodified
// values can be filtered out from a SystemConfig update. This can prevent
// repeatedly unmarshaling and processing the same SystemConfig values.
//
// A SystemConfigDeltaFilter is not safe for concurrent use by multiple
// goroutines.
type SystemConfigDeltaFilter struct {
	keyPrefix roachpb.Key
	lastCfg   config.SystemConfigEntries
}

// MakeSystemConfigDeltaFilter creates a new SystemConfigDeltaFilter. The filter
// will ignore all key-values without the specified key prefix, if one is
// provided.
func MakeSystemConfigDeltaFilter(keyPrefix roachpb.Key) SystemConfigDeltaFilter {
	return SystemConfigDeltaFilter{
		keyPrefix: keyPrefix,
	}
}

// ForModified calls the provided function for all SystemConfig kvs that were modified
// since the last call to this method.
func (df *SystemConfigDeltaFilter) ForModified(
	newCfg *config.SystemConfig, fn func(kv roachpb.KeyValue),
) {
	// Save newCfg in the filter.
	lastCfg := df.lastCfg
	df.lastCfg.Values = newCfg.Values

	// SystemConfig values are always sorted by key, so scan over new and old
	// configs in order to find new keys and modified values. Before doing so,
	// skip all keys in each list of values that are less than the keyPrefix.
	lastIdx, newIdx := 0, 0
	if df.keyPrefix != nil {
		lastIdx = sort.Search(len(lastCfg.Values), func(i int) bool {
			return bytes.Compare(lastCfg.Values[i].Key, df.keyPrefix) >= 0
		})
		newIdx = sort.Search(len(newCfg.Values), func(i int) bool {
			return bytes.Compare(newCfg.Values[i].Key, df.keyPrefix) >= 0
		})
	}

	for {
		if newIdx == len(newCfg.Values) {
			// All out of new keys.
			break
		}

		newKV := newCfg.Values[newIdx]
		if df.keyPrefix != nil && !bytes.HasPrefix(newKV.Key, df.keyPrefix) {
			// All out of new keys matching prefix.
			break
		}

		if lastIdx < len(lastCfg.Values) {
			oldKV := lastCfg.Values[lastIdx]
			switch oldKV.Key.Compare(newKV.Key) {
			case -1:
				// Deleted key.
				lastIdx++
			case 0:
				if !newKV.Value.EqualTagAndData(oldKV.Value) {
					// Modified value.
					fn(newKV)
				}
				lastIdx++
				newIdx++
			case 1:
				// New key.
				fn(newKV)
				newIdx++
			}
		} else {
			// New key.
			fn(newKV)
			newIdx++
		}
	}
}

// batchAndConsume waits on a channel to allow batching more events. It keeps
// while consuming events as they come to avoid blocking the channel producer.
func batchAndConsume(ch <-chan struct{}, batchDuration time.Duration) {
	var batchTimer timeutil.Timer
	defer batchTimer.Stop()
	batchTimer.Reset(batchDuration)
	for !batchTimer.Read {
		select {
		case <-ch: // event happened while we are waiting for our batchTimer to end.
		case <-batchTimer.C:
			batchTimer.Read = true
		}
	}
}
