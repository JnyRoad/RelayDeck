//go:build unit

package service

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var codexModelsManifestCacheTestOneMiBBody = bytes.Repeat([]byte{'x'}, 1<<20)

func codexModelsManifestCacheTestManifest(size int) *CodexModelsManifest {
	var body []byte
	if size == len(codexModelsManifestCacheTestOneMiBBody) {
		body = codexModelsManifestCacheTestOneMiBBody
	} else {
		body = bytes.Repeat([]byte{'x'}, size)
	}
	return &CodexModelsManifest{Body: body}
}

func codexModelsManifestCacheTestManifestWithSource(bodySize, sourceSize int) *CodexModelsManifest {
	return &CodexModelsManifest{
		Body:               bytes.Repeat([]byte{'x'}, bodySize),
		upstreamSourceBody: bytes.Repeat([]byte{'y'}, sourceSize),
	}
}

func TestCodexModelsManifestCacheTotalByteBudgetEvictsOldestEntry(t *testing.T) {
	cache := codexModelsManifestCache{}
	now := time.Unix(1_000, 0)
	entrySize := 1 << 20

	for i := 0; i < 64; i++ {
		cache.set(fmt.Sprintf("entry-%d", i), codexModelsManifestCacheTestManifest(entrySize), now)
	}
	require.EqualValues(t, codexModelsManifestCacheMaxBytes, cache.totalBytes)
	cache.set("entry-64", codexModelsManifestCacheTestManifest(entrySize), now)
	require.EqualValues(t, codexModelsManifestCacheMaxBytes, cache.totalBytes)

	oldest, state := cache.get("entry-0", now.Add(time.Second))
	require.Nil(t, oldest, "the 64 MiB budget must evict the oldest full-size entry")
	require.Equal(t, codexModelsManifestCacheMiss, state)
	newest, state := cache.get("entry-64", now.Add(time.Second))
	require.NotNil(t, newest)
	require.Equal(t, codexModelsManifestCacheFresh, state)
}

func TestCodexModelsManifestCacheReplacementReclaimsByteBudget(t *testing.T) {
	cache := codexModelsManifestCache{}
	now := time.Unix(2_000, 0)
	entrySize := 1 << 20

	for i := 0; i < 64; i++ {
		cache.set(fmt.Sprintf("entry-%d", i), codexModelsManifestCacheTestManifest(entrySize), now)
	}
	require.EqualValues(t, codexModelsManifestCacheMaxBytes, cache.totalBytes)
	cache.set("entry-0", codexModelsManifestCacheTestManifest(1), now.Add(time.Second))
	require.EqualValues(t, codexModelsManifestCacheMaxBytes-entrySize+1, cache.totalBytes)
	cache.set("entry-64", codexModelsManifestCacheTestManifest(entrySize), now.Add(2*time.Second))
	require.EqualValues(t, codexModelsManifestCacheMaxBytes-entrySize+1, cache.totalBytes)
	cache.set("entry-65", codexModelsManifestCacheTestManifest(entrySize), now.Add(3*time.Second))
	require.EqualValues(t, codexModelsManifestCacheMaxBytes-entrySize+1, cache.totalBytes)

	retained, state := cache.get("entry-0", now.Add(4*time.Second))
	require.NotNil(t, retained, "replacing an entry must release its previous bytes")
	require.Equal(t, codexModelsManifestCacheFresh, state)
	evicted, state := cache.get("entry-1", now.Add(4*time.Second))
	require.Nil(t, evicted, "the oldest remaining entry must be evicted after the replacement")
	require.Equal(t, codexModelsManifestCacheMiss, state)
}

func TestCodexModelsManifestCacheExpirationReclaimsByteBudget(t *testing.T) {
	cache := codexModelsManifestCache{}
	now := time.Unix(3_000, 0)
	entrySize := 1 << 20

	for i := 0; i < 64; i++ {
		cache.set(fmt.Sprintf("expired-%d", i), codexModelsManifestCacheTestManifest(entrySize), now)
	}
	expiredAt := now.Add(codexModelsManifestCacheStaleTTL + time.Second)
	for i := 0; i < 64; i++ {
		manifest, state := cache.get(fmt.Sprintf("expired-%d", i), expiredAt)
		require.Nil(t, manifest)
		require.Equal(t, codexModelsManifestCacheMiss, state)
	}
	require.Zero(t, cache.totalBytes, "expired entries must release both cached bodies from the byte count")

	for i := 0; i < 65; i++ {
		cache.set(fmt.Sprintf("new-%d", i), codexModelsManifestCacheTestManifest(entrySize), now.Add(time.Minute))
	}
	checkAt := now.Add(90 * time.Second)
	oldest, state := cache.get("new-0", checkAt)
	require.Nil(t, oldest, "expired entries must not consume the total byte budget")
	require.Equal(t, codexModelsManifestCacheMiss, state)
	newest, state := cache.get("new-64", checkAt)
	require.NotNil(t, newest)
	require.Equal(t, codexModelsManifestCacheFresh, state)
}

func TestCodexModelsManifestCacheCountsUpstreamSourceBody(t *testing.T) {
	cache := codexModelsManifestCache{}
	now := time.Unix(3_500, 0)
	cache.set("source", codexModelsManifestCacheTestManifestWithSource(512, 512), now)

	require.EqualValues(t, 1024, cache.totalBytes)
	cache.set("source", codexModelsManifestCacheTestManifestWithSource(256, 128), now.Add(time.Second))
	require.EqualValues(t, 384, cache.totalBytes, "replacement accounting must include Body and upstreamSourceBody")
}

func TestCodexModelsManifestCacheEnforcesPerEntryBodyAndSourceBoundary(t *testing.T) {
	cache := codexModelsManifestCache{}
	now := time.Unix(3_600, 0)
	half := codexModelsManifestCacheBodyLimit / 2

	cache.set("at-limit", codexModelsManifestCacheTestManifestWithSource(half, half), now)
	require.EqualValues(t, codexModelsManifestCacheBodyLimit, cache.totalBytes)

	cache.set("over-limit", codexModelsManifestCacheTestManifestWithSource(half, half+1), now)
	manifest, state := cache.get("over-limit", now.Add(time.Second))
	require.Nil(t, manifest)
	require.Equal(t, codexModelsManifestCacheMiss, state)
	require.EqualValues(t, codexModelsManifestCacheBodyLimit, cache.totalBytes)
}

func TestCodexModelsManifestCacheRetains512SmallEntries(t *testing.T) {
	cache := codexModelsManifestCache{}
	now := time.Unix(4_000, 0)
	for i := 0; i < codexModelsManifestCacheMaxEntries; i++ {
		cache.set(fmt.Sprintf("small-%d", i), codexModelsManifestCacheTestManifest(1), now)
	}

	for i := 0; i < codexModelsManifestCacheMaxEntries; i++ {
		manifest, state := cache.get(fmt.Sprintf("small-%d", i), now.Add(time.Second))
		require.NotNil(t, manifest, "small entries should not be evicted before the 512-entry limit")
		require.Equal(t, codexModelsManifestCacheFresh, state)
	}
}
