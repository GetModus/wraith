# Queue and Deduplication

## Queue Structure

The queue is a durable array of `Capture` structs persisted to `wraith-queue.json` in the data directory. It is rewritten (as indented JSON) on every mutation. Thread safety is provided by `sync.RWMutex`.

### Capture Fields

```go
type Capture struct {
    ID             string                 // Random 16-hex-char identifier
    Source         string                 // "extension", "context-menu", "bookmark-sync"
    URL            string
    Title          string
    SiteName       string
    Author         string
    BodyText       string                 // Extracted page text
    Selected       string                 // User-selected text, if any
    Headings       []CaptureHeading       // {Level, Text}
    Links          []CaptureLink          // {Text, Href}
    Images         []CaptureImage         // {Src, Alt, Width, Height}
    Tweet          *CaptureTweet          // X/Twitter-specific data (nullable)
    Meta           map[string]interface{} // Arbitrary metadata
    CapturedAt     string                 // RFC3339 timestamp
    Status         string                 // queued, processing, done, failed, deduped, discarded
    IngestState    string                 // captured, queued, deduped, processing, triaged, filed, discarded, mission_candidate, failed
    IngestHistory  []IngestTransition     // State transition ledger
    Fingerprint    string                 // Deterministic dedup key
    DuplicateOf    string                 // Points to canonical capture ID
    DuplicateCount int                    // How many duplicates folded into this capture
    LastSeenAt     string                 // Updated on each duplicate sighting
    Error          string                 // Error message on failure
    VaultPath      string                 // Where the file was written
}
```

### State Machine

Captures transition through these states:

```
captured -> queued -> processing -> triaged -> filed (done)
                  \-> deduped (at queue ingress or consumer dedup)
                  \-> processing -> failed
                  \-> processing -> triaged -> discarded (scout discard)
                  \-> processing -> triaged -> mission_candidate -> filed
```

Each transition is recorded as an `IngestTransition` with state, timestamp, and optional note. The `IngestHistory` array on each capture is the complete audit trail.

## Fingerprint Algorithm

Fingerprints provide exact-match deduplication at queue ingress. The algorithm:

1. Normalize inputs: lowercase, trim whitespace
2. Extract tweet ID if present (empty string otherwise)
3. Normalize URL: strip protocol (`https://`, `http://`), strip `www.` prefix, strip trailing `/`
4. Truncate body text to first 512 characters
5. Concatenate with pipe delimiter: `source|tweetID|URL|title|body[:512]`
6. SHA-256 hash the concatenated string
7. Take the first 12 bytes (24 hex chars) as the fingerprint

Implementation: `buildFingerprint()` in `queue.go`.

When a capture arrives with a fingerprint matching an existing non-failed capture, the existing capture's `DuplicateCount` is incremented, `LastSeenAt` is updated, and the new capture is recorded with `Status: "deduped"` and `DuplicateOf` pointing to the canonical capture ID. Both captures remain in the queue for audit purposes.

## Near-Duplicate Algorithm

Near-dedup catches captures that differ in minor runtime noise (query params, whitespace, dynamic content) but represent the same page. It runs after fingerprint dedup fails.

1. Normalize URLs of the incoming capture and all existing captures (same normalization as fingerprint: strip protocol, www, trailing slash)
2. Only compare captures with **matching normalized URLs** -- this is a gate to avoid O(n^2) full-text comparison
3. For matching-URL pairs, compute Jaccard word similarity:
   - Combine `title + " " + body[:1024]` for both captures (lowercase, trimmed)
   - Tokenize by splitting on whitespace
   - Strip punctuation from each token
   - **Discard tokens shorter than 3 characters**
   - Compute Jaccard index: `|intersection| / |union|`
4. If similarity **>= 0.82**, fold the new capture into the best-matching existing capture

The similarity score is recorded in `Meta["near_duplicate_similarity"]` on the deduped capture.

Implementation: `findNearDuplicate()`, `captureSimilarity()`, `jaccardWordSimilarity()`, `tokenizeForSimilarity()` in `queue.go`.

## Prune Behavior

`Queue.Prune(olderThan)` removes completed captures (status: done, failed, deduped, or discarded) whose `CapturedAt` is older than the specified duration. Queued and processing captures are never pruned. The queue file is rewritten after pruning.

## Pending Retrieval

`Queue.Pending(limit)` returns captures with `Status == "queued"`, sorted oldest-first. Default limit is 50.

## Queue Statistics

`Queue.Stats()` returns counts by status (total, queued, processing, done, failed, deduped, discarded) and the timestamp of the most recent capture.
