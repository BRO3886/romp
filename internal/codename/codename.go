// Package codename derives a stable, human-friendly identifier for a job.
package codename

import (
	"fmt"
	"hash/fnv"
)

// For returns an "adjective_name" pair for a repo and issue.
// It is deterministic, so the same issue always yields the same name.
func For(repo string, issue int) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s/%d", repo, issue)
	sum := h.Sum64()
	adj := adjectives[sum%uint64(len(adjectives))]
	name := nouns[(sum>>32)%uint64(len(nouns))]
	return adj + "_" + name
}

var adjectives = []string{
	"agile", "bright", "calm", "clever", "cosmic", "daring", "eager", "fearless",
	"festive", "gentle", "glowing", "golden", "happy", "jolly", "keen", "lively",
	"lucky", "merry", "mighty", "nimble", "peaceful", "playful", "proud", "quick",
	"quiet", "radiant", "rapid", "serene", "sprightly", "steady", "sunny", "swift",
}

var nouns = []string{
	"naruto", "sasuke", "sakura", "kakashi", "itachi", "jiraiya", "tsunade", "orochimaru",
	"gaara", "hinata", "shikamaru", "neji", "madara", "obito", "minato", "hashirama",
	"tobirama", "guy", "kiba", "choji", "shino", "tenten", "ino", "nagato",
	"konan", "kisame", "deidara", "kabuto", "zabuza", "haku", "asuma", "iruka",
}
