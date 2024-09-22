package reporulesetbot

import (
	"os"
	"testing"

	"github.com/google/go-github/v65/github"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestShouldProcessBypassActor(t *testing.T) {
	bypassActor := &github.BypassActor{}
	bypassActor.ActorID = github.Int64(6)
	assert.True(t, shouldProcessBypassActor(bypassActor))

	bypassActor.ActorID = github.Int64(4)
	assert.False(t, shouldProcessBypassActor(bypassActor))
}

func TestIsManagedRuleset(t *testing.T) {
	logger := zerolog.New(os.Stdout)

	event := &RulesetEvent{Ruleset: &github.Ruleset{Name: "test_ruleset"}, Organization: &github.Organization{Login: github.String("orgName")}}
	ruleset := &github.Ruleset{Name: "test_ruleset"}

	assert.True(t, isManagedRuleset(event, ruleset, logger))

	ruleset.Name = "other_ruleset"
	assert.False(t, isManagedRuleset(event, ruleset, logger))
}
