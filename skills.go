package romp

import "embed"

// EmbeddedSkills contains the agent skill distributed with Romp.
//
//go:embed skills/rompify
var EmbeddedSkills embed.FS
